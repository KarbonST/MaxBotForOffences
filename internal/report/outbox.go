package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Sink interface {
	Store(context.Context, DialogPayload) error
}

type OutboxConfig struct {
	Dir       string
	QueueSize int
	RetryBase time.Duration
	RetryMax  time.Duration
}

type Outbox struct {
	cfg     OutboxConfig
	sink    Sink
	logger  *slog.Logger
	pending string
	sent    string
	failed  string
	queue   chan string
	started bool
	startMu sync.Mutex
}

func NewOutbox(cfg OutboxConfig, sink Sink, logger *slog.Logger) (*Outbox, error) {
	if sink == nil {
		return nil, fmt.Errorf("outbox sink is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Dir == "" {
		cfg.Dir = "var/report_outbox"
	}
	if cfg.QueueSize < 1 {
		cfg.QueueSize = 256
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = time.Second
	}
	if cfg.RetryMax < cfg.RetryBase {
		cfg.RetryMax = 30 * time.Second
	}

	pendingDir := filepath.Join(cfg.Dir, "pending")
	sentDir := filepath.Join(cfg.Dir, "sent")
	failedDir := filepath.Join(cfg.Dir, "failed")

	for _, dir := range []string{cfg.Dir, pendingDir, sentDir, failedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create outbox dir %s: %w", dir, err)
		}
	}

	return &Outbox{
		cfg:     cfg,
		sink:    sink,
		logger:  logger,
		pending: pendingDir,
		sent:    sentDir,
		failed:  failedDir,
		queue:   make(chan string, cfg.QueueSize),
	}, nil
}

func (o *Outbox) Start(ctx context.Context) error {
	o.startMu.Lock()
	defer o.startMu.Unlock()
	if o.started {
		return nil
	}
	o.started = true

	files, err := os.ReadDir(o.pending)
	if err != nil {
		return fmt.Errorf("read pending outbox: %w", err)
	}

	paths := make([]string, 0, len(files))
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(o.pending, entry.Name()))
	}
	sort.Strings(paths)

	go o.worker(ctx)

	for _, path := range paths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case o.queue <- path:
		}
	}

	return nil
}

func (o *Outbox) Store(ctx context.Context, payload DialogPayload) error {
	payload.Normalize(time.Now())
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal dialog payload: %w", err)
	}

	name := makeOutboxFileName(payload)
	pendingPath := filepath.Join(o.pending, name)
	tmpPath := pendingPath + ".tmp"

	if err := os.WriteFile(tmpPath, raw, 0o644); err != nil {
		return fmt.Errorf("write outbox tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, pendingPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("move outbox file: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case o.queue <- pendingPath:
		return nil
	}
}

func (o *Outbox) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case path := <-o.queue:
			o.processWithRetry(ctx, path)
		}
	}
}

func (o *Outbox) processWithRetry(ctx context.Context, path string) {
	attempt := 1
	for {
		err := o.processFile(ctx, path)
		if err == nil {
			return
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}

		delay := o.backoffWithJitter(attempt)
		o.logger.Warn(
			"повтор отправки JSON-диалога в БД",
			"path", path,
			"attempt", attempt,
			"delay_ms", delay.Milliseconds(),
			"error", err.Error(),
		)

		if err := sleepWithContext(ctx, delay); err != nil {
			return
		}
		attempt++
	}
}

func (o *Outbox) processFile(ctx context.Context, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read outbox file: %w", err)
	}

	var payload DialogPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		o.logger.Error("некорректный JSON outbox, переносим в failed", "path", path, "error", err.Error())
		if moveErr := o.move(path, filepath.Join(o.failed, filepath.Base(path))); moveErr != nil {
			return fmt.Errorf("move invalid outbox file: %w", moveErr)
		}
		return nil
	}

	payload.Normalize(time.Now())
	if err := o.sink.Store(ctx, payload); err != nil {
		return err
	}

	return o.move(path, filepath.Join(o.sent, filepath.Base(path)))
}

func (o *Outbox) move(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}

	raw, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.WriteFile(to, raw, 0o644); err != nil {
		return err
	}
	return os.Remove(from)
}

func (o *Outbox) backoffWithJitter(attempt int) time.Duration {
	delay := o.cfg.RetryBase
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= o.cfg.RetryMax {
			delay = o.cfg.RetryMax
			break
		}
	}
	if delay > o.cfg.RetryMax {
		delay = o.cfg.RetryMax
	}

	jitterMax := delay / 4
	if jitterMax <= 0 {
		return delay
	}
	return delay + time.Duration(rand.Int63n(int64(jitterMax)))
}

func makeOutboxFileName(payload DialogPayload) string {
	key := strings.ReplaceAll(payload.DedupKey, string(filepath.Separator), "_")
	key = strings.ReplaceAll(key, ":", "_")
	if key == "" {
		key = "dialog"
	}
	return fmt.Sprintf("%d_%s.json", payload.CompletedAt.UnixNano(), key)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
