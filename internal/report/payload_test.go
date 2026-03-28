package report

import (
	"testing"
	"time"
)

func TestDialogPayloadNormalize(t *testing.T) {
	now := time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC)
	payload := DialogPayload{
		UserID:       77,
		ReportNumber: "DEV-77",
	}

	payload.Normalize(now)

	if payload.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentSchemaVersion, payload.SchemaVersion)
	}
	if payload.Source != "max_bot" {
		t.Fatalf("expected source max_bot, got %q", payload.Source)
	}
	if payload.CreatedAt.IsZero() || payload.CompletedAt.IsZero() {
		t.Fatalf("expected non-zero timestamps after normalize")
	}
	if payload.DialogID == "" {
		t.Fatalf("expected generated dialog id")
	}
	if payload.DedupKey != "77:DEV-77" {
		t.Fatalf("expected dedup key 77:DEV-77, got %q", payload.DedupKey)
	}
}
