-- Migration: 알람 타입별 구독 + 알림 템플릿 시스템
-- Date: 2026-01-21

-- 1. AlarmType ENUM 생성
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'alarm_type') THEN
        CREATE TYPE alarm_type AS ENUM ('LIVE', 'COMMUNITY', 'SHORTS');
    END IF;
END $$;

-- 2. alarms 테이블에 alarm_types 컬럼 추가 (기존 데이터는 LIVE 기본)
ALTER TABLE alarms
    ADD COLUMN IF NOT EXISTS alarm_types alarm_type[] NOT NULL DEFAULT ARRAY['LIVE']::alarm_type[];

-- 3. GIN 인덱스 추가 (배열 검색 최적화)
CREATE INDEX IF NOT EXISTS idx_alarms_alarm_types_gin ON alarms USING GIN (alarm_types);

-- 4. 알림 템플릿 테이블 생성
CREATE TABLE IF NOT EXISTS notification_templates (
    id           BIGSERIAL PRIMARY KEY,
    template_key VARCHAR(50) NOT NULL,
    channel_id   VARCHAR(64),
    body         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 5. 기본 템플릿 유니크 인덱스 (template_key당 1개)
CREATE UNIQUE INDEX IF NOT EXISTS ux_notification_templates_default
    ON notification_templates(template_key)
    WHERE channel_id IS NULL;

-- 6. 채널별 override 유니크 인덱스
CREATE UNIQUE INDEX IF NOT EXISTS ux_notification_templates_channel
    ON notification_templates(template_key, channel_id)
    WHERE channel_id IS NOT NULL;

-- 7. 기본 템플릿 seed
INSERT INTO notification_templates(template_key, channel_id, body)
VALUES
    ('OUTBOX_SHORTS', NULL, '[{{.MemberName}}] 새 쇼츠
{{.Title | truncate 50}}
{{.URL}}'),
    ('OUTBOX_COMMUNITY', NULL, '[{{.MemberName}}] 커뮤니티
{{.ContentText | truncate 100}}
{{.URL}}'),
    ('OUTBOX_VIDEO', NULL, '[{{.MemberName}}] 새 영상
{{.Title | truncate 50}}
{{.URL}}'),
    ('OUTBOX_MILESTONE', NULL, '[{{.MemberName}}] {{.Milestone}} 돌파!')
ON CONFLICT DO NOTHING;

-- 8. Outbox retry 지원 컬럼 추가
ALTER TABLE youtube_notification_outbox
    ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- 9. retry 조회용 인덱스
CREATE INDEX IF NOT EXISTS idx_yno_status_next_attempt
    ON youtube_notification_outbox(status, next_attempt_at, created_at);

-- 10. 기존 사용자 alarm_types 마이그레이션 (LIVE만 → 전체로 업그레이드)
UPDATE alarms
SET alarm_types = ARRAY['LIVE', 'COMMUNITY', 'SHORTS']::alarm_type[]
WHERE alarm_types = ARRAY['LIVE']::alarm_type[];

-- 11. 코멘트
COMMENT ON COLUMN alarms.alarm_types IS '구독 알람 타입 (LIVE, COMMUNITY, SHORTS)';
COMMENT ON TABLE notification_templates IS '알림 메시지 템플릿 (기본 + 채널별 override)';
COMMENT ON COLUMN notification_templates.template_key IS '템플릿 식별자 (OUTBOX_SHORTS, OUTBOX_COMMUNITY 등)';
COMMENT ON COLUMN notification_templates.channel_id IS 'NULL=기본 템플릿, 값=채널별 override';
COMMENT ON COLUMN youtube_notification_outbox.attempt_count IS '발송 시도 횟수';
COMMENT ON COLUMN youtube_notification_outbox.next_attempt_at IS '다음 재시도 가능 시각';
