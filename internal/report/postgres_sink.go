package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresSink struct {
	db *sql.DB
}

func NewPostgresSink(dsn string) (*PostgresSink, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("report postgres dsn is empty")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	store := &PostgresSink{db: db}
	if err := store.ensureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *PostgresSink) Close() error {
	return s.db.Close()
}

func (s *PostgresSink) Store(ctx context.Context, payload DialogPayload) error {
	payload.Normalize(time.Now())
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload for postgres: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO dialog_reports (
			dedup_key,
			dialog_id,
			user_id,
			report_number,
			schema_version,
			source,
			created_at,
			completed_at,
			message_id,
			normalized_at,
			payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
		ON CONFLICT (dedup_key) DO UPDATE SET
			report_number = CASE
				WHEN EXCLUDED.message_id IS NOT NULL THEN EXCLUDED.report_number
				ELSE dialog_reports.report_number
			END,
			message_id = COALESCE(dialog_reports.message_id, EXCLUDED.message_id),
			normalized_at = COALESCE(dialog_reports.normalized_at, EXCLUDED.normalized_at),
			payload = CASE
				WHEN EXCLUDED.message_id IS NOT NULL THEN EXCLUDED.payload
				ELSE dialog_reports.payload
			END
	`,
		payload.DedupKey,
		payload.DialogID,
		payload.UserID,
		payload.ReportNumber,
		payload.SchemaVersion,
		payload.Source,
		payload.CreatedAt,
		payload.CompletedAt,
		payload.MessageID,
		payload.NormalizedAt,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert dialog report: %w", err)
	}

	return nil
}

func (s *PostgresSink) ensureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS dialog_reports (
			id BIGSERIAL PRIMARY KEY,
			dedup_key TEXT NOT NULL UNIQUE,
			dialog_id TEXT NOT NULL,
			user_id BIGINT NOT NULL,
			report_number TEXT NOT NULL,
			schema_version INTEGER NOT NULL,
			source TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			completed_at TIMESTAMPTZ NOT NULL,
			message_id BIGINT,
			normalized_at TIMESTAMPTZ,
			payload JSONB NOT NULL,
			inserted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE dialog_reports ADD COLUMN IF NOT EXISTS message_id BIGINT;
		ALTER TABLE dialog_reports ADD COLUMN IF NOT EXISTS normalized_at TIMESTAMPTZ;

		CREATE INDEX IF NOT EXISTS idx_dialog_reports_user_id ON dialog_reports (user_id);
		CREATE INDEX IF NOT EXISTS idx_dialog_reports_message_id ON dialog_reports (message_id);
		CREATE INDEX IF NOT EXISTS idx_dialog_reports_completed_at ON dialog_reports (completed_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("ensure dialog_reports schema: %w", err)
	}
	return nil
}
