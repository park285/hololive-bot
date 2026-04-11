-- 053_create_youtube_community_shorts_source_posts.sql
-- 검증용 원본 수집 목록: 운영 채널에서 감지된 community/shorts 게시물의 canonical post identifier와 채널 정보를 보존한다.

CREATE TABLE IF NOT EXISTS youtube_community_shorts_source_posts (
    kind                VARCHAR(20) NOT NULL,
    post_id             VARCHAR(50) NOT NULL,
    channel_id          VARCHAR(50) NOT NULL,
    actual_published_at TIMESTAMPTZ,
    detected_at         TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (kind, post_id)
);

CREATE INDEX IF NOT EXISTS idx_ycssp_detected_at
    ON youtube_community_shorts_source_posts(detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycssp_channel_detected
    ON youtube_community_shorts_source_posts(channel_id, detected_at DESC);

COMMENT ON TABLE youtube_community_shorts_source_posts IS '유튜브 community/shorts 관찰 구간 검증용 원본 게시물 수집 목록';
COMMENT ON COLUMN youtube_community_shorts_source_posts.post_id IS '검증용 canonical post identifier';
COMMENT ON COLUMN youtube_community_shorts_source_posts.channel_id IS '게시물을 감지한 운영 채널 ID';
COMMENT ON COLUMN youtube_community_shorts_source_posts.actual_published_at IS '확보된 경우의 실제 게시 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_community_shorts_source_posts.detected_at IS '내부 시스템이 게시물을 처음 감지한 시각 (UTC canonical)';
