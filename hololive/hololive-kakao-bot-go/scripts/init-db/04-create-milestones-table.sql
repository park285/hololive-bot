-- youtube_milestones 테이블: 구독자 마일스톤 달성 기록
-- 중복 달성 방지 (구독자 감소 후 재증가 시에도 기존 달성 유지)

CREATE TABLE IF NOT EXISTS youtube_milestones (
    id SERIAL PRIMARY KEY,
    channel_id VARCHAR(24) NOT NULL,
    member_name TEXT NOT NULL,
    type VARCHAR(20) NOT NULL,  -- 'subscribers'
    value BIGINT NOT NULL,
    achieved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notified BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT youtube_milestones_unique UNIQUE(channel_id, type, value)
);

-- 최신 달성 기록 조회용 인덱스
CREATE INDEX IF NOT EXISTS idx_milestones_achieved_at 
    ON youtube_milestones (achieved_at DESC);

-- 채널별 조회용 인덱스
CREATE INDEX IF NOT EXISTS idx_milestones_channel 
    ON youtube_milestones (channel_id);

-- 미발송 알림 조회용 부분 인덱스
CREATE INDEX IF NOT EXISTS idx_milestones_unnotified 
    ON youtube_milestones (notified) WHERE notified = false;

COMMENT ON TABLE youtube_milestones IS '유튜브 채널 구독자 마일스톤 달성 기록';
COMMENT ON COLUMN youtube_milestones.achieved_at IS '시스템 기록 시각 (실제 달성과 최대 1-12시간 오차 가능)';
