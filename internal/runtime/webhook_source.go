package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"max_bot/internal/maxapi"
)

const webhookSecretHeader = "X-Max-Bot-Api-Secret"

type WebhookConfig struct {
	Addr            string
	Path            string
	Secret          string
	QueueSize       int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type WebhookSource struct {
	cfg    WebhookConfig
	logger *slog.Logger
}

func NewWebhookSource(cfg WebhookConfig, logger *slog.Logger) *WebhookSource {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.QueueSize < 1 {
		cfg.QueueSize = 512
	}
	return &WebhookSource{cfg: cfg, logger: logger}
}

func (s *WebhookSource) Run(ctx context.Context, handler UpdateHandler) error {
	httpHandler, cleanup := s.newHTTPHandler(ctx, handler)
	defer cleanup()

	server := &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      httpHandler,
		ReadTimeout:  s.cfg.ReadTimeout,
		WriteTimeout: s.cfg.WriteTimeout,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	s.logger.Info("webhook source started", "addr", s.cfg.Addr, "path", s.cfg.Path)

	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return err
}

func (s *WebhookSource) newHTTPHandler(ctx context.Context, handler UpdateHandler) (http.Handler, func()) {
	ready := atomic.Bool{}
	ready.Store(true)

	queue := make(chan maxapi.Update, s.cfg.QueueSize)
	workerCtx, workerCancel := context.WithCancel(ctx)

	go func() {
		for {
			select {
			case <-workerCtx.Done():
				return
			case update := <-queue:
				if err := handler(workerCtx, update); err != nil {
					s.logger.Error("webhook update handler failed", "type", update.UpdateType, "error", err.Error())
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ready")
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "not ready")
	})

	mux.HandleFunc(s.cfg.Path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if s.cfg.Secret != "" && r.Header.Get(webhookSecretHeader) != s.cfg.Secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var update maxapi.Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		select {
		case queue <- update:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok")
		default:
			http.Error(w, "queue is full", http.StatusServiceUnavailable)
		}
	})

	return mux, workerCancel
}
