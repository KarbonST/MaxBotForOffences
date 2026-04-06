package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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
	MarkerFile     string
	MarkerStore    MarkerStore
}

type PollingSource struct {
	client *maxapi.Client
	cfg    PollingConfig
	logger *slog.Logger
	store  MarkerStore
}

func NewPollingSource(client *maxapi.Client, cfg PollingConfig, logger *slog.Logger) *PollingSource {
	if logger == nil {
		logger = slog.Default()
	}

	return &PollingSource{
		client: client,
		cfg:    cfg,
		logger: logger,
		store:  resolveMarkerStore(cfg),
	}
}

func resolveMarkerStore(cfg PollingConfig) MarkerStore {
	if cfg.MarkerStore != nil {
		return cfg.MarkerStore
	}
	if strings.TrimSpace(cfg.MarkerFile) == "" {
		return nil
	}
	return NewFileMarkerStore(cfg.MarkerFile)
}

func (s *PollingSource) Run(ctx context.Context, handler UpdateHandler) error {
	var marker *int64
	if s.store != nil {
		loaded, err := s.store.Load()
		if err != nil {
			s.logger.Warn("не удалось загрузить polling marker", "error", err.Error())
		} else if loaded != nil {
			marker = loaded
			s.logger.Info("восстановлен polling marker", "marker", *loaded)
		}
	}
	cycles := 0

	s.logger.Info(
		"источник polling запущен",
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
			s.logger.Warn("ошибка запроса polling", "error", err.Error())
			if err := sleepWithContext(ctx, 2*time.Second); err != nil {
				return nil
			}
			continue
		}

		if updates.Marker != nil {
			marker = updates.Marker
			if s.store != nil {
				if err := s.store.Save(*updates.Marker); err != nil {
					s.logger.Warn("не удалось сохранить polling marker", "marker", *updates.Marker, "error", err.Error())
				}
			}
		}

		if len(updates.Updates) == 0 && s.cfg.LogEmptyPolls {
			s.logger.Info("цикл polling: обновлений нет")
		}

		for _, update := range updates.Updates {
			if err := handler(ctx, update); err != nil {
				s.logger.Error("ошибка обработчика обновления", "type", update.UpdateType, "error", err.Error())
			}
		}

		cycles++
		if s.cfg.PollOnce {
			s.logger.Info("polling остановлен: режим одного цикла")
			return nil
		}
		if s.cfg.PollMaxCycles > 0 && cycles >= s.cfg.PollMaxCycles {
			s.logger.Info("polling остановлен: достигнут лимит циклов", "cycles", cycles)
			return nil
		}
	}
}
