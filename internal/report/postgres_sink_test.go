package report

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresSinkStoreUpsertsLinkedPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	sink := &PostgresSink{db: db}
	messageID := int64(15)
	normalizedAt := time.Date(2026, time.March, 31, 15, 4, 5, 0, time.UTC)
	payload := DialogPayload{
		SchemaVersion: CurrentSchemaVersion,
		DialogID:      "dlg-777",
		DedupKey:      "777:RAW-1",
		Source:        "max_bot",
		UserID:        777,
		ReportNumber:  "15",
		CreatedAt:     time.Date(2026, time.March, 31, 15, 0, 0, 0, time.UTC),
		CompletedAt:   time.Date(2026, time.March, 31, 15, 1, 0, 0, time.UTC),
		MessageID:     &messageID,
		NormalizedAt:  &normalizedAt,
		Draft: DialogDraft{
			CategoryName: "Категория",
		},
	}

	mock.ExpectExec(`(?s)`+regexp.QuoteMeta(`INSERT INTO dialog_reports`)+`.*`+regexp.QuoteMeta(`message_id = COALESCE(dialog_reports.message_id, EXCLUDED.message_id)`)+`.*`+regexp.QuoteMeta(`normalized_at = COALESCE(dialog_reports.normalized_at, EXCLUDED.normalized_at)`)+`.*`+regexp.QuoteMeta(`WHEN EXCLUDED.message_id IS NOT NULL THEN EXCLUDED.payload`)).
		WithArgs(
			payload.DedupKey,
			payload.DialogID,
			payload.UserID,
			payload.ReportNumber,
			payload.SchemaVersion,
			payload.Source,
			payload.CreatedAt,
			payload.CompletedAt,
			messageID,
			normalizedAt,
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := sink.Store(context.Background(), payload); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
