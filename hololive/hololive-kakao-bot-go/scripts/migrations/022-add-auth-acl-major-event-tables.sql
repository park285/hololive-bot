-- 022-add-auth-acl-major-event-tables.sql
-- 런타임 DDL 제거를 위해 애플리케이션이 생성하던 테이블을 마이그레이션으로 이전
-- 모든 변경은 additive/idempotent로 구성하여 기존 데이터 보존

-- ============================================================================
-- ACL tables
-- ============================================================================
CREATE TABLE IF NOT EXISTS acl_settings (
    id SERIAL PRIMARY KEY,
    key VARCHAR(64) UNIQUE NOT NULL,
    value TEXT
);

CREATE TABLE IF NOT EXISTS acl_rooms (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) UNIQUE NOT NULL
);

-- ============================================================================
-- Auth tables
-- ============================================================================
CREATE TABLE IF NOT EXISTS auth_users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    avatar_url TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS auth_password_reset_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    used_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 토큰 정리/검증 쿼리 최적화
CREATE INDEX IF NOT EXISTS idx_auth_reset_tokens_user_unused
    ON auth_password_reset_tokens (user_id)
    WHERE used_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_auth_reset_tokens_valid_lookup
    ON auth_password_reset_tokens (token_hash, expires_at)
    WHERE used_at IS NULL;

-- ============================================================================
-- Major event tables
-- ============================================================================
CREATE TABLE IF NOT EXISTS major_event_subscriptions (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(255) UNIQUE NOT NULL,
    room_name VARCHAR(255),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS major_events (
    id SERIAL PRIMARY KEY,
    external_id VARCHAR(500) UNIQUE NOT NULL,
    type VARCHAR(20) DEFAULT 'event',
    title VARCHAR(500) NOT NULL,
    link VARCHAR(1000) NOT NULL,
    description TEXT,
    members TEXT[],
    pub_date TIMESTAMPTZ,
    event_start_date DATE,
    event_end_date DATE,
    status VARCHAR(50) DEFAULT 'active',
    notified_at TIMESTAMPTZ,
    notified_week VARCHAR(10),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'major_events'
          AND column_name = 'type'
    ) THEN
        ALTER TABLE major_events ADD COLUMN type VARCHAR(20) DEFAULT 'event';
    END IF;
END $$;

UPDATE major_events
SET type = 'event'
WHERE type IS NULL;

ALTER TABLE major_events
    ALTER COLUMN type SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_major_events_start_date ON major_events(event_start_date);
CREATE INDEX IF NOT EXISTS idx_major_events_status ON major_events(status);
CREATE INDEX IF NOT EXISTS idx_major_events_notified ON major_events(notified_week);
CREATE INDEX IF NOT EXISTS idx_major_events_type ON major_events(type);
