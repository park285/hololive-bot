ALTER TABLE bot_reply_outbox
    ADD COLUMN IF NOT EXISTS operator_replay_grants INTEGER NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_operator_replay_grants'
    ) THEN
        ALTER TABLE bot_reply_outbox
            ADD CONSTRAINT chk_bot_reply_outbox_operator_replay_grants
            CHECK (operator_replay_grants >= 0) NOT VALID;
    END IF;
END
$$;

ALTER TABLE bot_reply_outbox
    VALIDATE CONSTRAINT chk_bot_reply_outbox_operator_replay_grants;
