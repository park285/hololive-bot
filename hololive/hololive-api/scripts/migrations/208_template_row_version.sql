-- 타임스탬프 충돌 없이 모든 writer의 본문/식별자 변경을 구분합니다.
BEGIN;
ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS row_version BIGINT NOT NULL DEFAULT 1;

CREATE OR REPLACE FUNCTION notification_template_row_version() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.body, NEW.template_key, NEW.channel_id, NEW.id)
        IS DISTINCT FROM (OLD.body, OLD.template_key, OLD.channel_id, OLD.id) THEN
        NEW.row_version := OLD.row_version + 1;
    ELSE
        NEW.row_version := OLD.row_version;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE TRIGGER notification_template_row_version_trigger
BEFORE UPDATE ON notification_templates
FOR EACH ROW EXECUTE FUNCTION notification_template_row_version();
COMMIT;
