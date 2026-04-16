-- Основа схемы приведена к актуальной версии БД (2026-04-14.sql).
-- Наполнение справочников (категории и муниципалитеты) находится в 001_reference_schema.sql.
-- Техническая raw-таблица dialog_reports вынесена в 002_dialog_reports.sql.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'message_status') THEN
        CREATE TYPE message_status AS ENUM (
            'draft',
            'moderation',
            'in_progress',
            'clarification_requested',
            'rejected',
            'resolved'
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'message_stage') THEN
        CREATE TYPE message_stage AS ENUM (
            'category',
            'municipality',
            'phone',
            'address',
            'time',
            'description',
            'files',
            'additional',
            'sended'
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'admin_role') THEN
        CREATE TYPE admin_role AS ENUM ('admin', 'superadmin');
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'history_event_type') THEN
        CREATE TYPE history_event_type AS ENUM (
            'status',
            'category',
            'municipality'
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'request_status') THEN
        CREATE TYPE request_status AS ENUM (
            'new',
            'answered',
            'rejected'
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'notification_status') THEN
        CREATE TYPE notification_status AS ENUM (
            'new',
            'sended',
            'error'
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'user_stage') THEN
        CREATE TYPE user_stage AS ENUM (
            'main_menu',
            'filling_report',
            'viewing_messages',
            'waiting_clarification'
        );
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    max_id BIGINT UNIQUE NOT NULL,
    stage user_stage NOT NULL DEFAULT 'main_menu',
    temp_category INTEGER,
    previous_stage user_stage,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS municipalities (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (name <> ''),
    sorting SMALLINT UNIQUE NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS categories (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (name <> ''),
    sorting SMALLINT UNIQUE NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS admins (
    id SERIAL PRIMARY KEY,
    login TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    last_name TEXT NOT NULL,
    first_name TEXT NOT NULL,
    surname TEXT,
    role admin_role NOT NULL DEFAULT 'admin',
    municipality_id INTEGER[],
    last_login TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    is_active BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id SERIAL PRIMARY KEY,
    admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    revoked BOOLEAN DEFAULT FALSE,
    UNIQUE(token_hash)
);

CREATE TABLE IF NOT EXISTS messages (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id INTEGER,
    municipality_id INTEGER,
    status message_status NOT NULL DEFAULT 'draft',
    phone TEXT,
    address TEXT,
    incident_time TEXT,
    description TEXT,
    additional_info TEXT,
    stage message_stage NOT NULL,
    answer TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    sended_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT NOW(),

    CONSTRAINT messages_phone_check CHECK (
        phone IS NULL OR phone ~ '^\d{10}$'
    ),

    CONSTRAINT messages_complete_check CHECK (
        status != 'moderation' OR (
            category_id IS NOT NULL AND
            municipality_id IS NOT NULL AND
            phone IS NOT NULL AND
            address IS NOT NULL AND
            incident_time IS NOT NULL AND
            description IS NOT NULL
        )
    )
);

CREATE TABLE IF NOT EXISTS messages_history (
    id SERIAL PRIMARY KEY,
    message_id INTEGER NOT NULL,
    admin_id INTEGER,
    date TIMESTAMP NOT NULL DEFAULT NOW(),
    event_type history_event_type NOT NULL,
    value JSONB NOT NULL,
    notification_id INTEGER,
    comment TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS files (
    id SERIAL PRIMARY KEY,
    message_id INTEGER NOT NULL,
    path TEXT NOT NULL,
    file_name TEXT,
    file_size INTEGER,
    mime_type TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS clarification_requests (
    id SERIAL PRIMARY KEY,
    message_id INTEGER NOT NULL,
    admin_id INTEGER NOT NULL,
    answer TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP,
    status request_status DEFAULT 'new'
);

CREATE TABLE IF NOT EXISTS user_notifications (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    notification TEXT NOT NULL,
    status notification_status NOT NULL DEFAULT 'new',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    sended_at TIMESTAMP
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_messages_category') THEN
        ALTER TABLE messages
            ADD CONSTRAINT fk_messages_category
            FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE RESTRICT;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_messages_municipality') THEN
        ALTER TABLE messages
            ADD CONSTRAINT fk_messages_municipality
            FOREIGN KEY (municipality_id) REFERENCES municipalities(id) ON DELETE RESTRICT;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_history_message') THEN
        ALTER TABLE messages_history
            ADD CONSTRAINT fk_history_message
            FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_history_admin') THEN
        ALTER TABLE messages_history
            ADD CONSTRAINT fk_history_admin
            FOREIGN KEY (admin_id) REFERENCES admins(id) ON DELETE SET NULL;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_history_notification') THEN
        ALTER TABLE messages_history
            ADD CONSTRAINT fk_history_notification
            FOREIGN KEY (notification_id) REFERENCES user_notifications(id) ON DELETE SET NULL;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_files_message') THEN
        ALTER TABLE files
            ADD CONSTRAINT fk_files_message
            FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_clarification_message') THEN
        ALTER TABLE clarification_requests
            ADD CONSTRAINT fk_clarification_message
            FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_clarification_admin') THEN
        ALTER TABLE clarification_requests
            ADD CONSTRAINT fk_clarification_admin
            FOREIGN KEY (admin_id) REFERENCES admins(id) ON DELETE RESTRICT;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_notifications_user') THEN
        ALTER TABLE user_notifications
            ADD CONSTRAINT fk_notifications_user
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_users_max_id ON users(max_id);
CREATE INDEX IF NOT EXISTS idx_users_stage ON users(stage);

CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_status ON messages(status);
CREATE INDEX IF NOT EXISTS idx_messages_drafts ON messages(user_id) WHERE status = 'draft';
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_messages_sended ON messages(sended_at) WHERE sended_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_history_message ON messages_history(message_id);
CREATE INDEX IF NOT EXISTS idx_history_admin ON messages_history(admin_id);
CREATE INDEX IF NOT EXISTS idx_history_notification ON messages_history(notification_id);

CREATE INDEX IF NOT EXISTS idx_files_message ON files(message_id);

CREATE INDEX IF NOT EXISTS idx_clarification_message ON clarification_requests(message_id);
CREATE INDEX IF NOT EXISTS idx_clarification_status ON clarification_requests(status);

CREATE INDEX IF NOT EXISTS idx_notifications_user ON user_notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_status ON user_notifications(status);

CREATE INDEX IF NOT EXISTS idx_categories_sorting ON categories(sorting);
CREATE INDEX IF NOT EXISTS idx_municipalities_sorting ON municipalities(sorting);
CREATE INDEX IF NOT EXISTS idx_categories_active ON categories(is_active);
CREATE INDEX IF NOT EXISTS idx_municipalities_active ON municipalities(is_active);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_admin_id ON refresh_tokens(admin_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS update_users_updated_at ON users;
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_messages_updated_at ON messages;
CREATE TRIGGER update_messages_updated_at
    BEFORE UPDATE ON messages
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_admins_updated_at ON admins;
CREATE TRIGGER update_admins_updated_at
    BEFORE UPDATE ON admins
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE OR REPLACE FUNCTION delete_old_messages() RETURNS void AS $$
BEGIN
    DELETE FROM messages
    WHERE status IN ('moderation', 'rejected', 'resolved')
      AND sended_at < NOW() - INTERVAL '30 days';

    DELETE FROM messages
    WHERE status = 'draft'
      AND created_at < NOW() - INTERVAL '30 days';
END;
$$ LANGUAGE plpgsql;
