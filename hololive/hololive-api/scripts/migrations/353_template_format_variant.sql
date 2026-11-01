-- 템플릿 본문을 방 유형별 변형으로 나눈다. 카카오 일반채팅은 RichText 서식을 표시하지 않으므로 일반 텍스트로
-- 보내는 방은 plain, 확인된 오픈채팅의 RichText 발송은 richtext 본문을 쓴다. 기존 행은 모두 plain이 된다.
-- 행 추가 전에 unique index를 354~357에서 (template_key[, channel_id], format_variant)로 바꾸고, 358이 행을 나눈다.
BEGIN;
ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS format_variant TEXT NOT NULL DEFAULT 'plain';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_constraint
        WHERE conrelid = 'public.notification_templates'::regclass
          AND conname = 'notification_templates_format_variant_check'
    ) THEN
        ALTER TABLE notification_templates
            ADD CONSTRAINT notification_templates_format_variant_check CHECK (format_variant IN ('plain', 'richtext'));
    END IF;
END;
$$;

-- 변형 변경도 렌더러 캐시가 새 본문으로 보도록 row_version 비교 대상에 넣는다.
CREATE OR REPLACE FUNCTION notification_template_row_version() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.body, NEW.template_key, NEW.channel_id, NEW.format_variant, NEW.id)
        IS DISTINCT FROM (OLD.body, OLD.template_key, OLD.channel_id, OLD.format_variant, OLD.id) THEN
        NEW.row_version := OLD.row_version + 1;
    ELSE
        NEW.row_version := OLD.row_version;
    END IF;
    RETURN NEW;
END;
$$;
COMMIT;
