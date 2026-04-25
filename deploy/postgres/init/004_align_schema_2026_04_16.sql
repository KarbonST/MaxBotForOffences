-- Приведение ранее развёрнутой схемы к версии 2026-04-16.sql.
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
    ADD COLUMN IF NOT EXISTS clarification_id INTEGER;

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

UPDATE messages_history
SET value = jsonb_build_object('old_value', NULL, 'new_value', NULL)
WHERE value IS NULL;

ALTER TABLE messages_history
    ALTER COLUMN value SET NOT NULL;

ALTER TABLE messages_history
    DROP COLUMN IF EXISTS comments;

ALTER TABLE messages_history
    DROP COLUMN IF EXISTS new_value;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_history_clarification'
    ) THEN
        ALTER TABLE messages_history
            ADD CONSTRAINT fk_history_clarification
            FOREIGN KEY (clarification_id) REFERENCES clarification_requests(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_history_clarification ON messages_history(clarification_id);

CREATE OR REPLACE FUNCTION handle_notification_error()
RETURNS TRIGGER AS $$
DECLARE
    target_message_id INTEGER;
    old_message_status message_status;
BEGIN
    IF NEW.status = 'error' AND OLD.status IS DISTINCT FROM 'error' THEN
        SELECT m.id, m.status INTO target_message_id, old_message_status
        FROM messages m
        JOIN messages_history mh ON mh.message_id = m.id
        WHERE mh.notification_id = NEW.id
          AND mh.event_type = 'status'
          AND mh.value->>'new_value' = 'clarification_requested'
        LIMIT 1;

        IF target_message_id IS NOT NULL AND old_message_status != 'in_progress' THEN
            UPDATE messages
            SET status = 'in_progress',
                updated_at = NOW()
            WHERE id = target_message_id;

            INSERT INTO messages_history (
                message_id,
                event_type,
                value,
                comment,
                notification_id
            ) VALUES (
                target_message_id,
                'status',
                jsonb_build_object(
                    'old_value', old_message_status,
                    'new_value', 'in_progress'
                ),
                'Clarification notification failed to send (error status)',
                NEW.id
            );
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notification_error_to_in_progress ON user_notifications;
CREATE TRIGGER trg_notification_error_to_in_progress
    AFTER UPDATE OF status ON user_notifications
    FOR EACH ROW
    EXECUTE FUNCTION handle_notification_error();

CREATE OR REPLACE FUNCTION handle_clarification_response()
RETURNS TRIGGER AS $$
DECLARE
    old_message_status message_status;
BEGIN
    IF NEW.status IN ('answered', 'rejected') AND OLD.status IS DISTINCT FROM NEW.status THEN
        SELECT status INTO old_message_status
        FROM messages
        WHERE id = NEW.message_id;

        IF old_message_status != 'in_progress' THEN
            UPDATE messages
            SET status = 'in_progress',
                updated_at = NOW()
            WHERE id = NEW.message_id;

            INSERT INTO messages_history (
                message_id,
                event_type,
                value,
                clarification_id,
                comment
            ) VALUES (
                NEW.message_id,
                'status',
                jsonb_build_object(
                    'old_value', old_message_status,
                    'new_value', 'in_progress'
                ),
                NEW.id,
                CASE NEW.status
                    WHEN 'answered' THEN 'Response to clarification request received'
                    WHEN 'rejected' THEN 'Clarification request rejected by user'
                END
            );
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_clarification_response_to_in_progress ON clarification_requests;
CREATE TRIGGER trg_clarification_response_to_in_progress
    AFTER UPDATE OF status ON clarification_requests
    FOR EACH ROW
    EXECUTE FUNCTION handle_clarification_response();
