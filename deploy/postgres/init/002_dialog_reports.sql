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
    payload JSONB NOT NULL,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dialog_reports_user_id ON dialog_reports (user_id);
CREATE INDEX IF NOT EXISTS idx_dialog_reports_completed_at ON dialog_reports (completed_at DESC);
