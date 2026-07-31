CREATE OR REPLACE FUNCTION scrub_bot_webhook_inbox_terminal_payload()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.payload := '{}'::jsonb;
    RETURN NEW;
END
$$;

CREATE OR REPLACE TRIGGER bot_webhook_inbox_terminal_payload_scrub
    BEFORE INSERT OR UPDATE OF status, payload
    ON bot_webhook_inbox
    FOR EACH ROW
    WHEN (NEW.status IN ('dead', 'succeeded'))
    EXECUTE FUNCTION scrub_bot_webhook_inbox_terminal_payload();

UPDATE bot_webhook_inbox
SET payload = '{}'::jsonb
WHERE status IN ('dead', 'succeeded')
  AND payload IS DISTINCT FROM '{}'::jsonb;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'bot_webhook_inbox'::regclass
          AND conname = 'chk_bot_webhook_inbox_terminal_payload_scrubbed'
    ) THEN
        ALTER TABLE bot_webhook_inbox
            ADD CONSTRAINT chk_bot_webhook_inbox_terminal_payload_scrubbed
            CHECK (status NOT IN ('dead', 'succeeded') OR payload = '{}'::jsonb) NOT VALID;
    END IF;
END
$$;

ALTER TABLE bot_webhook_inbox
    VALIDATE CONSTRAINT chk_bot_webhook_inbox_terminal_payload_scrubbed;
