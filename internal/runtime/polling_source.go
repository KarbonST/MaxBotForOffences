package runtime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"max_bot/internal/maxapi"
)

type PollingConfig struct {
	TimeoutSeconds int
	Limit          int
	PollOnce       bool
	PollMaxCycles  int
	LogEmptyPolls  bool
	UpdateTypes    []string
}

type PollingSource struct {
	client *maxapi.Client
	cfg    PollingConfig
	logger *slog.Logger
}

func NewPollingSource(client *maxapi.Client, cfg PollingConfig, logger *slog.Logger) *PollingSource {
	if logger == nil {
		logger = slog.Default()
	}

	return &PollingSource{
		client: client,
		cfg:    cfg,
		logger: logger,
	}
}

func (s *PollingSource) Run(ctx context.Context, handler UpdateHandler) error {
	var marker *int64
	cycles := 0

	s.logger.Info(
		"polling source started",
		"timeout_sec", s.cfg.TimeoutSeconds,
		"limit", s.cfg.Limit,
		"poll_once", s.cfg.PollOnce,
		"poll_max_cycles", s.cfg.PollMaxCycles,
	)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		pollCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds+5)*time.Second)
		updates, err := s.client.GetUpdates(pollCtx, marker, s.cfg.TimeoutSeconds, s.cfg.Limit, s.cfg.UpdateTypes)
		cancel()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			s.logger.Warn("polling request failed", "error", err.Error())
			if err := sleepWithContext(ctx, 2*time.Second); err != nil {
				return nil
			}
			continue
		}

		if updates.Marker != nil {
			marker = updates.Marker
		}

		if len(updates.Updates) == 0 && s.cfg.LogEmptyPolls {
			s.logger.Info("poll tick: no updates")
		}

		for _, update := range updates.Updates {
			if err := handler(ctx, update); err != nil {
				s.logger.Error("update handler failed", "type", update.UpdateType, "error", err.Error())
			}
		}

		cycles++
		if s.cfg.PollOnce {
			s.logger.Info("polling stopped: single poll mode")
			return nil
		}
		if s.cfg.PollMaxCycles > 0 && cycles >= s.cfg.PollMaxCycles {
			s.logger.Info("polling stopped: max cycles reached", "cycles", cycles)
			return nil
		}
	}
}
