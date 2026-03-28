package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("MAX_BOT_TOKEN", "token")
	t.Setenv("MAX_RUN_MODE", "")
	t.Setenv("MAX_WEBHOOK_PATH", "")
	t.Setenv("MAX_HTTP_READ_TIMEOUT", "")
	t.Setenv("MAX_HTTP_WRITE_TIMEOUT", "")
	t.Setenv("MAX_SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RunMode != "polling" {
		t.Fatalf("expected polling run mode, got %q", cfg.RunMode)
	}
	if cfg.WebhookPath != "/webhook/max" {
		t.Fatalf("unexpected webhook path: %q", cfg.WebhookPath)
	}
	if cfg.PollTimeout != 30 || cfg.PollLimit != 100 {
		t.Fatalf("unexpected poll defaults: timeout=%d limit=%d", cfg.PollTimeout, cfg.PollLimit)
	}
	if cfg.HTTPReadTimeout != 10*time.Second || cfg.HTTPWriteTimeout != 10*time.Second {
		t.Fatalf("unexpected http timeout defaults: read=%v write=%v", cfg.HTTPReadTimeout, cfg.HTTPWriteTimeout)
	}
	if cfg.APIMaxRetries != 3 {
		t.Fatalf("unexpected retry default: %d", cfg.APIMaxRetries)
	}
	if cfg.ReferenceAPIBaseURL != "http://127.0.0.1:8090" {
		t.Fatalf("unexpected reference api base: %q", cfg.ReferenceAPIBaseURL)
	}
	if cfg.ReferenceAPITimeout != 5*time.Second {
		t.Fatalf("unexpected reference api timeout: %v", cfg.ReferenceAPITimeout)
	}
	if !cfg.ReportPipelineEnabled {
		t.Fatalf("expected report pipeline enabled by default")
	}
	if cfg.ReportOutboxDir != "var/report_outbox" {
		t.Fatalf("unexpected report outbox dir: %q", cfg.ReportOutboxDir)
	}
	if cfg.ReportOutboxQueueSize != 256 {
		t.Fatalf("unexpected report outbox queue: %d", cfg.ReportOutboxQueueSize)
	}
}

func TestLoadNormalizesInvalidValues(t *testing.T) {
	t.Setenv("MAX_BOT_TOKEN", "token")
	t.Setenv("MAX_RUN_MODE", "invalid")
	t.Setenv("MAX_POLL_TIMEOUT", "999")
	t.Setenv("MAX_POLL_LIMIT", "0")
	t.Setenv("MAX_POLL_MAX_CYCLES", "-5")
	t.Setenv("MAX_WEBHOOK_PATH", "webhook")
	t.Setenv("LOG_FORMAT", "invalid")
	t.Setenv("MAX_WEBHOOK_QUEUE_SIZE", "0")
	t.Setenv("MAX_API_MAX_RETRIES", "-2")
	t.Setenv("MAX_API_RETRY_BASE_MS", "0")
	t.Setenv("MAX_API_RETRY_MAX_MS", "1")
	t.Setenv("MAX_DEDUP_TTL", "0")
	t.Setenv("REFERENCE_API_TIMEOUT", "0")
	t.Setenv("REFERENCE_CACHE_TTL", "0")
	t.Setenv("REPORT_OUTBOX_QUEUE_SIZE", "0")
	t.Setenv("REPORT_OUTBOX_RETRY_BASE", "0")
	t.Setenv("REPORT_OUTBOX_RETRY_MAX", "1ms")
	t.Setenv("REPORT_OUTBOX_DIR", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RunMode != "polling" {
		t.Fatalf("expected fallback run mode polling, got %q", cfg.RunMode)
	}
	if cfg.PollTimeout != 30 {
		t.Fatalf("expected poll timeout fallback, got %d", cfg.PollTimeout)
	}
	if cfg.PollLimit != 100 {
		t.Fatalf("expected poll limit fallback, got %d", cfg.PollLimit)
	}
	if cfg.PollMaxCycles != 0 {
		t.Fatalf("expected poll max cycles fallback, got %d", cfg.PollMaxCycles)
	}
	if cfg.WebhookPath != "/webhook" {
		t.Fatalf("expected normalized webhook path, got %q", cfg.WebhookPath)
	}
	if cfg.LogFormat != "text" {
		t.Fatalf("expected log format fallback text, got %q", cfg.LogFormat)
	}
	if cfg.WebhookQueueSize != 512 {
		t.Fatalf("expected queue fallback 512, got %d", cfg.WebhookQueueSize)
	}
	if cfg.APIMaxRetries != 1 {
		t.Fatalf("expected retries fallback 1, got %d", cfg.APIMaxRetries)
	}
	if cfg.DedupTTL <= 0 {
		t.Fatalf("expected positive dedup ttl, got %v", cfg.DedupTTL)
	}
	if cfg.ReferenceAPITimeout <= 0 {
		t.Fatalf("expected positive reference api timeout, got %v", cfg.ReferenceAPITimeout)
	}
	if cfg.ReferenceCacheTTL <= 0 {
		t.Fatalf("expected positive reference cache ttl, got %v", cfg.ReferenceCacheTTL)
	}
	if cfg.ReportOutboxQueueSize != 256 {
		t.Fatalf("expected report outbox queue fallback, got %d", cfg.ReportOutboxQueueSize)
	}
	if cfg.ReportOutboxRetryBase <= 0 {
		t.Fatalf("expected positive report outbox retry base, got %v", cfg.ReportOutboxRetryBase)
	}
	if cfg.ReportOutboxRetryMax < cfg.ReportOutboxRetryBase {
		t.Fatalf("expected report outbox retry max >= base, got base=%v max=%v", cfg.ReportOutboxRetryBase, cfg.ReportOutboxRetryMax)
	}
	if cfg.ReportOutboxDir != "var/report_outbox" {
		t.Fatalf("expected report outbox dir fallback, got %q", cfg.ReportOutboxDir)
	}
}

func TestLoadDurationParsing(t *testing.T) {
	t.Setenv("MAX_BOT_TOKEN", "token")
	t.Setenv("MAX_HTTP_READ_TIMEOUT", "3s")
	t.Setenv("MAX_HTTP_WRITE_TIMEOUT", "4")
	t.Setenv("MAX_SHUTDOWN_TIMEOUT", "5s")
	t.Setenv("MAX_DEDUP_TTL", "7s")
	t.Setenv("REFERENCE_API_TIMEOUT", "6s")
	t.Setenv("REFERENCE_CACHE_TTL", "8s")
	t.Setenv("REPORT_OUTBOX_RETRY_BASE", "2s")
	t.Setenv("REPORT_OUTBOX_RETRY_MAX", "9")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPReadTimeout != 3*time.Second {
		t.Fatalf("expected read timeout 3s, got %v", cfg.HTTPReadTimeout)
	}
	if cfg.HTTPWriteTimeout != 4*time.Second {
		t.Fatalf("expected write timeout 4s from numeric value, got %v", cfg.HTTPWriteTimeout)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Fatalf("expected shutdown timeout 5s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.DedupTTL != 7*time.Second {
		t.Fatalf("expected dedup ttl 7s, got %v", cfg.DedupTTL)
	}
	if cfg.ReferenceAPITimeout != 6*time.Second {
		t.Fatalf("expected reference api timeout 6s, got %v", cfg.ReferenceAPITimeout)
	}
	if cfg.ReferenceCacheTTL != 8*time.Second {
		t.Fatalf("expected reference cache ttl 8s, got %v", cfg.ReferenceCacheTTL)
	}
	if cfg.ReportOutboxRetryBase != 2*time.Second {
		t.Fatalf("expected report outbox retry base 2s, got %v", cfg.ReportOutboxRetryBase)
	}
	if cfg.ReportOutboxRetryMax != 9*time.Second {
		t.Fatalf("expected report outbox retry max 9s, got %v", cfg.ReportOutboxRetryMax)
	}
}

func TestLoadReportDatabaseFallback(t *testing.T) {
	t.Setenv("MAX_BOT_TOKEN", "token")
	t.Setenv("REPORT_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/maxbot?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ReportDatabaseURL != "postgres://user:pass@localhost:5432/maxbot?sslmode=disable" {
		t.Fatalf("unexpected report database url fallback: %q", cfg.ReportDatabaseURL)
	}
}
