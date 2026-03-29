package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Token      string
	APIBaseURL string
	RunMode    string

	ReferenceAPIBaseURL   string
	ReferenceAPITimeout   time.Duration
	ReferenceCacheTTL     time.Duration
	CoreAPIBaseURL        string
	CoreAPITimeout        time.Duration
	ReportPipelineEnabled bool
	ReportDatabaseURL     string
	ReportOutboxDir       string
	ReportOutboxQueueSize int
	ReportOutboxRetryBase time.Duration
	ReportOutboxRetryMax  time.Duration

	PollTimeout   int
	PollLimit     int
	PollOnce      bool
	PollMaxCycles int
	LogEmptyPolls bool

	WebhookAddr      string
	WebhookPath      string
	WebhookSecret    string
	WebhookQueueSize int
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	ShutdownTimeout  time.Duration

	LogFormat string
	LogLevel  string

	APIMaxRetries int
	APIRetryBase  time.Duration
	APIRetryMax   time.Duration
	DedupTTL      time.Duration
	UpdateTypes   []string
}

func Load() (Config, error) {
	cfg := Config{
		Token:      os.Getenv("MAX_BOT_TOKEN"),
		APIBaseURL: getenv("MAX_API_BASE", "https://platform-api.max.ru"),
		RunMode:    strings.ToLower(getenv("MAX_RUN_MODE", "polling")),

		ReferenceAPIBaseURL:   getenv("REFERENCE_API_BASE", "http://127.0.0.1:8091"),
		ReferenceAPITimeout:   getenvDuration("REFERENCE_API_TIMEOUT", 5*time.Second),
		ReferenceCacheTTL:     getenvDuration("REFERENCE_CACHE_TTL", 5*time.Minute),
		CoreAPIBaseURL:        getenv("CORE_API_BASE", "http://127.0.0.1:8091"),
		CoreAPITimeout:        getenvDuration("CORE_API_TIMEOUT", 5*time.Second),
		ReportPipelineEnabled: getenvBool("REPORT_PIPELINE_ENABLED", true),
		ReportDatabaseURL:     strings.TrimSpace(getenv("REPORT_DATABASE_URL", os.Getenv("DATABASE_URL"))),
		ReportOutboxDir:       getenv("REPORT_OUTBOX_DIR", "var/report_outbox"),
		ReportOutboxQueueSize: getenvInt("REPORT_OUTBOX_QUEUE_SIZE", 256),
		ReportOutboxRetryBase: getenvDuration("REPORT_OUTBOX_RETRY_BASE", time.Second),
		ReportOutboxRetryMax:  getenvDuration("REPORT_OUTBOX_RETRY_MAX", 30*time.Second),

		PollTimeout:   getenvInt("MAX_POLL_TIMEOUT", 30),
		PollLimit:     getenvInt("MAX_POLL_LIMIT", 100),
		PollOnce:      getenvBool("MAX_POLL_ONCE", false),
		PollMaxCycles: getenvInt("MAX_POLL_MAX_CYCLES", 0),
		LogEmptyPolls: getenvBool("MAX_LOG_EMPTY_POLLS", false),

		WebhookAddr:      getenv("MAX_WEBHOOK_ADDR", ":8080"),
		WebhookPath:      getenv("MAX_WEBHOOK_PATH", "/webhook/max"),
		WebhookSecret:    os.Getenv("MAX_WEBHOOK_SECRET"),
		WebhookQueueSize: getenvInt("MAX_WEBHOOK_QUEUE_SIZE", 512),
		HTTPReadTimeout:  getenvDuration("MAX_HTTP_READ_TIMEOUT", 10*time.Second),
		HTTPWriteTimeout: getenvDuration("MAX_HTTP_WRITE_TIMEOUT", 10*time.Second),
		ShutdownTimeout:  getenvDuration("MAX_SHUTDOWN_TIMEOUT", 10*time.Second),

		LogFormat: strings.ToLower(getenv("LOG_FORMAT", "text")),
		LogLevel:  strings.ToLower(getenv("LOG_LEVEL", "info")),

		APIMaxRetries: getenvInt("MAX_API_MAX_RETRIES", 3),
		APIRetryBase:  time.Duration(getenvInt("MAX_API_RETRY_BASE_MS", 250)) * time.Millisecond,
		APIRetryMax:   time.Duration(getenvInt("MAX_API_RETRY_MAX_MS", 3000)) * time.Millisecond,
		DedupTTL:      getenvDuration("MAX_DEDUP_TTL", 10*time.Minute),
		UpdateTypes: []string{
			"bot_started",
			"message_created",
			"message_callback",
		},
	}

	if cfg.Token == "" {
		return Config{}, fmt.Errorf("обязательная переменная MAX_BOT_TOKEN не задана (задайте env или укажите MAX_BOT_TOKEN=... в .env)")
	}

	if cfg.PollTimeout < 0 || cfg.PollTimeout > 90 {
		cfg.PollTimeout = 30
	}

	if cfg.PollLimit < 1 || cfg.PollLimit > 1000 {
		cfg.PollLimit = 100
	}

	if cfg.PollMaxCycles < 0 {
		cfg.PollMaxCycles = 0
	}

	if cfg.RunMode != "polling" && cfg.RunMode != "webhook" {
		cfg.RunMode = "polling"
	}

	if !strings.HasPrefix(cfg.WebhookPath, "/") {
		cfg.WebhookPath = "/" + cfg.WebhookPath
	}

	if cfg.WebhookQueueSize < 1 {
		cfg.WebhookQueueSize = 512
	}

	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		cfg.LogFormat = "text"
	}

	if cfg.APIMaxRetries < 1 {
		cfg.APIMaxRetries = 1
	}
	if cfg.APIRetryBase <= 0 {
		cfg.APIRetryBase = 250 * time.Millisecond
	}
	if cfg.APIRetryMax < cfg.APIRetryBase {
		cfg.APIRetryMax = 3 * time.Second
	}
	if cfg.DedupTTL <= 0 {
		cfg.DedupTTL = 10 * time.Minute
	}
	if cfg.ReferenceAPITimeout <= 0 {
		cfg.ReferenceAPITimeout = 5 * time.Second
	}
	if cfg.ReferenceCacheTTL <= 0 {
		cfg.ReferenceCacheTTL = 5 * time.Minute
	}
	if cfg.CoreAPITimeout <= 0 {
		cfg.CoreAPITimeout = 5 * time.Second
	}
	if cfg.ReportOutboxQueueSize < 1 {
		cfg.ReportOutboxQueueSize = 256
	}
	if cfg.ReportOutboxRetryBase <= 0 {
		cfg.ReportOutboxRetryBase = time.Second
	}
	if cfg.ReportOutboxRetryMax < cfg.ReportOutboxRetryBase {
		cfg.ReportOutboxRetryMax = 30 * time.Second
	}
	if strings.TrimSpace(cfg.ReportOutboxDir) == "" {
		cfg.ReportOutboxDir = "var/report_outbox"
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getenvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	switch value {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON", "да", "ДА", "Да", "вкл", "ВКЛ", "Вкл":
		return true
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF", "нет", "НЕТ", "Нет", "выкл", "ВЫКЛ", "Выкл":
		return false
	default:
		return fallback
	}
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	if d, err := time.ParseDuration(value); err == nil {
		return d
	}

	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}

	return fallback
}
