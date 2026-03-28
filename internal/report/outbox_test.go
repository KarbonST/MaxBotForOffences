package report

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type sinkMock struct {
	mu        sync.Mutex
	payloads  []DialogPayload
	failTimes int
}

func (m *sinkMock) Store(_ context.Context, payload DialogPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failTimes > 0 {
		m.failTimes--
		return errors.New("temporary sink error")
	}
	m.payloads = append(m.payloads, payload)
	return nil
}

func (m *sinkMock) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.payloads)
}

func TestOutboxStoreAndFlush(t *testing.T) {
	dir := t.TempDir()
	sink := &sinkMock{}

	outbox, err := NewOutbox(OutboxConfig{
		Dir:       dir,
		QueueSize: 4,
		RetryBase: 10 * time.Millisecond,
		RetryMax:  20 * time.Millisecond,
	}, sink, slog.Default())
	if err != nil {
		t.Fatalf("NewOutbox() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := outbox.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = outbox.Store(ctx, DialogPayload{
		UserID:       1001,
		ReportNumber: "DEV-1",
		Draft: DialogDraft{
			CategoryName: "Категория",
		},
	})
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	waitFor(t, time.Second, func() bool { return sink.count() == 1 })

	sentFiles, err := os.ReadDir(filepath.Join(dir, "sent"))
	if err != nil {
		t.Fatalf("read sent dir: %v", err)
	}
	if len(sentFiles) != 1 {
		t.Fatalf("expected 1 sent file, got %d", len(sentFiles))
	}
}

func TestOutboxRetry(t *testing.T) {
	dir := t.TempDir()
	sink := &sinkMock{failTimes: 2}

	outbox, err := NewOutbox(OutboxConfig{
		Dir:       dir,
		QueueSize: 4,
		RetryBase: 5 * time.Millisecond,
		RetryMax:  10 * time.Millisecond,
	}, sink, slog.Default())
	if err != nil {
		t.Fatalf("NewOutbox() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := outbox.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := outbox.Store(ctx, DialogPayload{UserID: 1002, ReportNumber: "DEV-2"}); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool { return sink.count() == 1 })
}

func TestOutboxProcessesPendingOnStart(t *testing.T) {
	dir := t.TempDir()
	pendingDir := filepath.Join(dir, "pending")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	payload := DialogPayload{UserID: 500, ReportNumber: "DEV-500"}
	payload.Normalize(time.Now())

	rawPath := filepath.Join(pendingDir, "old.json")
	raw, err := jsonMarshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(rawPath, raw, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sink := &sinkMock{}
	outbox, err := NewOutbox(OutboxConfig{
		Dir:       dir,
		QueueSize: 4,
		RetryBase: 5 * time.Millisecond,
		RetryMax:  10 * time.Millisecond,
	}, sink, slog.Default())
	if err != nil {
		t.Fatalf("NewOutbox() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := outbox.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, time.Second, func() bool { return sink.count() == 1 })
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition was not met within %v", timeout)
}

func jsonMarshal(payload DialogPayload) ([]byte, error) {
	return json.Marshal(payload)
}
