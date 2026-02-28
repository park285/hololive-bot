-- youtube_stats_history 테이블 (TimescaleDB hypertable 대신 일반 테이블로 생성)
CREATE TABLE IF NOT EXISTS youtube_stats_history (
    time TIMESTAMP WITH TIME ZONE NOT NULL,
    channel_id VARCHAR(64) NOT NULL,
    member_name VARCHAR(100),
    subscribers BIGINT,
    videos BIGINT,
    views BIGINT,
    CONSTRAINT youtube_stats_history_pkey PRIMARY KEY (time, channel_id)
);

-- 최적화를 위한 인덱스 (hypertable 대신)
CREATE INDEX IF NOT EXISTS idx_youtube_stats_history_channel_time 
    ON youtube_stats_history (channel_id, time DESC);
CREATE INDEX IF NOT EXISTS idx_youtube_stats_history_time 
    ON youtube_stats_history (time DESC);

COMMENT ON TABLE youtube_stats_history IS 'YouTube 채널 통계 이력 (일반 PostgreSQL 테이블)';
