-- 054_create_youtube_community_shorts_observation_post_baselines.sql
-- 24시간 관찰 종료 시점의 community/shorts 확정 게시물 기준 목록을 저장한다.

ALTER TABLE youtube_community_shorts_observation_windows
    ADD COLUMN IF NOT EXISTS finalized_post_baseline_at TIMESTAMPTZ;

ALTER TABLE youtube_community_shorts_observation_windows
    ADD COLUMN IF NOT EXISTS finalized_post_count INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_ycsow_finalized_post_baseline_at
    ON youtube_community_shorts_observation_windows(finalized_post_baseline_at DESC);

COMMENT ON COLUMN youtube_community_shorts_observation_windows.finalized_post_baseline_at IS '24시간 관찰 종료 시점에 dedup 완료된 확정 게시물 기준 목록을 고정한 시각 (UTC canonical, observation_ended_at와 동일)';
COMMENT ON COLUMN youtube_community_shorts_observation_windows.finalized_post_count IS '관찰 종료 시점에 고정한 community/shorts 확정 게시물 기준 목록의 게시물 수';

CREATE TABLE IF NOT EXISTS youtube_community_shorts_observation_post_baselines (
    runtime_name VARCHAR(50) NOT NULL,
    bigbang_cutover_at TIMESTAMPTZ NOT NULL,
    kind VARCHAR(20) NOT NULL,
    post_id VARCHAR(50) NOT NULL,
    channel_id VARCHAR(50) NOT NULL,
    actual_published_at TIMESTAMPTZ,
    detected_at TIMESTAMPTZ NOT NULL,
    finalized_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (runtime_name, bigbang_cutover_at, kind, post_id),
    CONSTRAINT fk_ycsopb_observation_window
        FOREIGN KEY (runtime_name, bigbang_cutover_at)
        REFERENCES youtube_community_shorts_observation_windows(runtime_name, bigbang_cutover_at)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ycsopb_window_detected
    ON youtube_community_shorts_observation_post_baselines(runtime_name, bigbang_cutover_at, detected_at DESC);

CREATE INDEX IF NOT EXISTS idx_ycsopb_channel_detected
    ON youtube_community_shorts_observation_post_baselines(channel_id, detected_at DESC);

COMMENT ON TABLE youtube_community_shorts_observation_post_baselines IS '24시간 관찰 종료 시점에 고정한 유튜브 community/shorts 확정 게시물 기준 목록';
COMMENT ON COLUMN youtube_community_shorts_observation_post_baselines.post_id IS '관찰 종료 시점 기준의 canonical post identifier';
COMMENT ON COLUMN youtube_community_shorts_observation_post_baselines.detected_at IS '관찰 종료 시점까지 dedup 기준으로 확정된 최초 감지 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_community_shorts_observation_post_baselines.finalized_at IS '확정 기준 목록을 고정한 시각 (UTC canonical, observation_ended_at와 동일)';
