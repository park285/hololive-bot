-- 재시도에서 동일 ID의 본문·수신 방·membership을 보존한다. 기존 행은 자동 backfill하지 않는다.
CREATE TABLE IF NOT EXISTS youtube_notification_send_request (
    base_id TEXT PRIMARY KEY,
    room_id VARCHAR(100) NOT NULL,
    message TEXT NOT NULL,
    message_hash TEXT NOT NULL,
    route TEXT NOT NULL CHECK (route IN ('text', 'markdown', 'sender')),
    dedupe_keys TEXT[] NOT NULL,
    member_ids BIGINT[] NOT NULL,
    generation INTEGER NOT NULL DEFAULT 0 CHECK (generation BETWEEN 0 AND 2),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (cardinality(member_ids) > 0),
    CHECK (cardinality(dedupe_keys) > 0)
);
ALTER TABLE youtube_notification_delivery
    ADD COLUMN IF NOT EXISTS send_request_id TEXT REFERENCES youtube_notification_send_request(base_id);
-- 최초 도입 시 과거 행과 새 행을 구분한다. 기존 행 전체를 갱신하지 않는 fast default다.
ALTER TABLE youtube_notification_delivery
    ADD COLUMN IF NOT EXISTS request_snapshot_allowed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE youtube_notification_delivery ALTER COLUMN request_snapshot_allowed SET DEFAULT TRUE;
