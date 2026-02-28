-- 알람 테이블: Valkey 캐시의 영속 백업 저장소
-- 앱 시작 시 이 테이블에서 Valkey로 일괄 로드됨

CREATE TABLE IF NOT EXISTS alarms (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    channel_id VARCHAR(64) NOT NULL,
    member_name VARCHAR(200),           -- 알람 추가 시점의 멤버 표시명
    room_name VARCHAR(200),             -- 방 이름 (캐싱용)
    user_name VARCHAR(200),             -- 사용자 이름 (캐싱용)
    created_at TIMESTAMPTZ DEFAULT NOW(),
    
    CONSTRAINT alarms_unique UNIQUE(room_id, user_id, channel_id)
);

-- 사용자별 알람 조회용 인덱스
CREATE INDEX IF NOT EXISTS idx_alarms_room_user ON alarms(room_id, user_id);

-- 채널별 구독자 조회용 인덱스
CREATE INDEX IF NOT EXISTS idx_alarms_channel ON alarms(channel_id);

-- 코멘트 추가
COMMENT ON TABLE alarms IS '사용자별 방송 알람 구독 정보 (Valkey 영속 백업)';
COMMENT ON COLUMN alarms.member_name IS '알람 추가 시점의 멤버 표시명 (캐싱)';
