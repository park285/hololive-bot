-- 마일스톤 예고 알림 기능 추가
-- 99% 도달 시 예고 알림을 발송하고, 중복 발송을 방지하기 위한 테이블 추가

-- 마일스톤 접근 알림 기록 테이블 (달성 전 예고 알림 중복 방지)
CREATE TABLE IF NOT EXISTS youtube_milestone_approaching (
    id SERIAL PRIMARY KEY,
    channel_id VARCHAR(24) NOT NULL,
    milestone_value BIGINT NOT NULL,
    notified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    current_subs BIGINT NOT NULL,  -- 알림 발송 시점의 구독자 수
    chat_notified BOOLEAN NOT NULL DEFAULT false,  -- 채팅방 발송 완료 여부
    CONSTRAINT youtube_milestone_approaching_unique UNIQUE(channel_id, milestone_value)
);

-- 채널별 조회용 인덱스
CREATE INDEX IF NOT EXISTS idx_approaching_channel 
    ON youtube_milestone_approaching (channel_id);

-- 미발송 알림 조회용 부분 인덱스
CREATE INDEX IF NOT EXISTS idx_approaching_unnotified
    ON youtube_milestone_approaching (chat_notified) WHERE chat_notified = false;

COMMENT ON TABLE youtube_milestone_approaching IS '마일스톤 접근 예고 알림 기록 (99% 도달 시 발송)';
COMMENT ON COLUMN youtube_milestone_approaching.current_subs IS '예고 알림 발송 시점의 구독자 수';
COMMENT ON COLUMN youtube_milestone_approaching.chat_notified IS '채팅방 발송 완료 여부';
