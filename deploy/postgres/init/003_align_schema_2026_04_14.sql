-- Приведение ранее развёрнутой схемы к версии 2026-04-14.sql.
-- Скрипт идемпотентен и безопасен для повторного запуска.

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id SERIAL PRIMARY KEY,
    admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    revoked BOOLEAN DEFAULT FALSE,
    UNIQUE(token_hash)
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_admin_id ON refresh_tokens(admin_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

ALTER TABLE messages_history
    ADD COLUMN IF NOT EXISTS value JSONB;

ALTER TABLE messages_history
    ADD COLUMN IF NOT EXISTS comment TEXT;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'messages_history'
          AND column_name = 'new_value'
    ) THEN
        UPDATE messages_history
        SET value = COALESCE(
            value,
            jsonb_build_object('old_value', NULL, 'new_value', new_value)
        );
    END IF;

    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'messages_history'
          AND column_name = 'comments'
    ) THEN
        UPDATE messages_history
        SET comment = COALESCE(comment, comments);
    END IF;
END $$;

ALTER TABLE messages_history
    ALTER COLUMN value SET NOT NULL;

ALTER TABLE messages_history
    DROP COLUMN IF EXISTS comments;

ALTER TABLE messages_history
    DROP COLUMN IF EXISTS new_value;

DROP TRIGGER IF EXISTS update_draft_attachments_updated_at ON draft_attachments;
DROP INDEX IF EXISTS idx_draft_attachments_updated_at;
DROP TABLE IF EXISTS draft_attachments;

ALTER TABLE clarification_requests
    DROP CONSTRAINT IF EXISTS fk_clarification_admin;

ALTER TABLE clarification_requests
    DROP CONSTRAINT IF EXISTS clarification_requests_admin_id_fkey;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_clarification_admin'
    ) THEN
        ALTER TABLE clarification_requests
            ADD CONSTRAINT fk_clarification_admin
            FOREIGN KEY (admin_id) REFERENCES admins(id) ON DELETE RESTRICT;
    END IF;
END $$;
