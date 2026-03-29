package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"max_bot/internal/config"
	"max_bot/internal/maxapi"
	"max_bot/internal/reference"
	"max_bot/internal/report"
	"max_bot/internal/reporting"
	botruntime "max_bot/internal/runtime"
	"max_bot/internal/scenario"
)

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "ошибка загрузки .env: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "ошибка конфигурации: %v\n", err)
		os.Exit(1)
	}

	logger := buildLogger(cfg.LogFormat, cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := maxapi.NewClientWithOptions(cfg.APIBaseURL, cfg.Token, maxapi.ClientOptions{
		Logger: logger,
		Retry: maxapi.RetryConfig{
			MaxRetries: cfg.APIMaxRetries,
			BaseDelay:  cfg.APIRetryBase,
			MaxDelay:   cfg.APIRetryMax,
		},
	})

	referenceClient := reference.NewClient(cfg.ReferenceAPIBaseURL, reference.ClientOptions{
		HTTPClient: &http.Client{Timeout: cfg.ReferenceAPITimeout},
	})
	referenceProvider := reference.NewCachedProvider(referenceClient, cfg.ReferenceCacheTTL)
	reportClient := reporting.NewClient(cfg.CoreAPIBaseURL, reporting.ClientOptions{
		HTTPClient: &http.Client{Timeout: cfg.CoreAPITimeout},
	})

	engineOptions := make([]scenario.Option, 0, 2)
	closers := make([]io.Closer, 0, 1)
	defer closeAll(closers, logger)

	engineOptions = append(engineOptions, scenario.WithReportCreator(reportClient))

	if cfg.ReportPipelineEnabled {
		if cfg.ReportDatabaseURL == "" {
			logger.Warn("pipeline диалогов отключен: не задан REPORT_DATABASE_URL (или DATABASE_URL)")
		} else {
			postgresSink, err := report.NewPostgresSink(cfg.ReportDatabaseURL)
			if err != nil {
				logger.Error("не удалось инициализировать postgres sink диалогов", "error", err.Error())
				os.Exit(1)
			}
			closers = append(closers, postgresSink)

			outbox, err := report.NewOutbox(report.OutboxConfig{
				Dir:       cfg.ReportOutboxDir,
				QueueSize: cfg.ReportOutboxQueueSize,
				RetryBase: cfg.ReportOutboxRetryBase,
				RetryMax:  cfg.ReportOutboxRetryMax,
			}, postgresSink, logger)
			if err != nil {
				logger.Error("не удалось инициализировать outbox диалогов", "error", err.Error())
				os.Exit(1)
			}
			if err := outbox.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("не удалось запустить outbox диалогов", "error", err.Error())
				os.Exit(1)
			}

			engineOptions = append(engineOptions, scenario.WithReportSink(outbox))
			logger.Info(
				"pipeline диалогов включен",
				"outbox_dir", cfg.ReportOutboxDir,
				"outbox_queue", cfg.ReportOutboxQueueSize,
			)
		}
	}

	logger.Info(
		"подключены backend api",
		"reference_api_base", cfg.ReferenceAPIBaseURL,
		"core_api_base", cfg.CoreAPIBaseURL,
	)

	engine := scenario.New(client, referenceProvider, engineOptions...)
	deduper := botruntime.NewDeduper(cfg.DedupTTL)

	updateHandler := func(handlerCtx context.Context, update maxapi.Update) error {
		if deduper.Seen(update) {
			slog.Debug("дубликат обновления пропущен", "type", update.UpdateType)
			return nil
		}
		return engine.HandleUpdate(handlerCtx, update)
	}

	if info, err := client.GetMe(ctx); err != nil {
		slog.Warn("не удалось получить данные бота", "error", err.Error())
	} else {
		slog.Info("бот подключен", "id", info.UserID, "name", info.Name, "username", info.Username)
	}

	source := makeSource(cfg, client, logger)
	if err := source.Run(ctx, updateHandler); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("ошибка запуска источника обновлений", "mode", cfg.RunMode, "error", err.Error())
		os.Exit(1)
	}
}

func closeAll(closers []io.Closer, logger *slog.Logger) {
	for _, closer := range closers {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil {
			logger.Warn("ошибка закрытия ресурса", "error", err.Error())
		}
	}
}

func makeSource(cfg config.Config, client *maxapi.Client, logger *slog.Logger) botruntime.UpdateSource {
	switch cfg.RunMode {
	case "webhook":
		return botruntime.NewWebhookSource(botruntime.WebhookConfig{
			Addr:            cfg.WebhookAddr,
			Path:            cfg.WebhookPath,
			Secret:          cfg.WebhookSecret,
			QueueSize:       cfg.WebhookQueueSize,
			ReadTimeout:     cfg.HTTPReadTimeout,
			WriteTimeout:    cfg.HTTPWriteTimeout,
			ShutdownTimeout: cfg.ShutdownTimeout,
		}, logger)
	default:
		return botruntime.NewPollingSource(client, botruntime.PollingConfig{
			TimeoutSeconds: cfg.PollTimeout,
			Limit:          cfg.PollLimit,
			PollOnce:       cfg.PollOnce,
			PollMaxCycles:  cfg.PollMaxCycles,
			LogEmptyPolls:  cfg.LogEmptyPolls,
			UpdateTypes:    cfg.UpdateTypes,
		}, logger)
	}
}

func buildLogger(format, level string) *slog.Logger {
	logLevel := parseLogLevel(level)
	options := &slog.HandlerOptions{
		Level: logLevel,
	}

	switch strings.ToLower(format) {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, options))
	default:
		return slog.New(slog.NewTextHandler(os.Stdout, options))
	}
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
