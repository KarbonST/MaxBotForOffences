package report

import (
	"testing"
	"time"
)

func TestDialogPayloadNormalize(t *testing.T) {
	now := time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC)
	messageID := int64(15)
	normalizedAt := time.Date(2026, time.March, 28, 13, 30, 0, 0, time.FixedZone("+0300", 3*60*60))
	payload := DialogPayload{
		UserID:       77,
		ReportNumber: "DEV-77",
		MessageID:    &messageID,
		NormalizedAt: &normalizedAt,
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
	if payload.MessageID == nil || *payload.MessageID != 15 {
		t.Fatalf("expected message id 15, got %+v", payload.MessageID)
	}
	if payload.NormalizedAt == nil {
		t.Fatalf("expected normalized_at to remain set")
	}
	if !payload.NormalizedAt.Equal(normalizedAt.UTC()) {
		t.Fatalf("expected normalized_at in UTC, got %s", payload.NormalizedAt)
	}
}
