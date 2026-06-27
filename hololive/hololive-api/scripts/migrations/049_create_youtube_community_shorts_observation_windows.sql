-- 049_create_youtube_community_shorts_observation_windows.sql
-- 유튜브 커뮤니티/쇼츠 빅뱅 배포 관찰 구간 메타데이터 저장소 생성

CREATE TABLE IF NOT EXISTS youtube_community_shorts_observation_windows (
    runtime_name           VARCHAR(50) NOT NULL,
    bigbang_cutover_at     TIMESTAMPTZ NOT NULL,
    app_version            VARCHAR(100) NOT NULL,
    target_channel_count   INT NOT NULL,
    deployment_completed_at TIMESTAMPTZ NOT NULL,
    observation_started_at TIMESTAMPTZ NOT NULL,
    observation_ended_at   TIMESTAMPTZ NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (runtime_name, bigbang_cutover_at),
    CHECK (observation_ended_at > observation_started_at)
);

CREATE INDEX IF NOT EXISTS idx_ycsow_deploy_completed
    ON youtube_community_shorts_observation_windows(deployment_completed_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycsow_window_start
    ON youtube_community_shorts_observation_windows(observation_started_at DESC);
CREATE INDEX IF NOT EXISTS idx_ycsow_window_end
    ON youtube_community_shorts_observation_windows(observation_ended_at DESC);

COMMENT ON TABLE youtube_community_shorts_observation_windows IS '유튜브 커뮤니티/쇼츠 빅뱅 배포 완료와 24시간 관찰 구간 메타데이터';
COMMENT ON COLUMN youtube_community_shorts_observation_windows.bigbang_cutover_at IS '전체 운영 채널 빅뱅 cutover 기준 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_community_shorts_observation_windows.deployment_completed_at IS '실제 빅뱅 배포 완료를 런타임이 감지한 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_community_shorts_observation_windows.observation_started_at IS '배포 직후 시작한 관찰 구간 시작 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_community_shorts_observation_windows.observation_ended_at IS '관찰 구간 종료 시각 (UTC canonical, 시작+24h)';
