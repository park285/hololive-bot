CREATE OR REPLACE FUNCTION scrub_bot_command_execution_terminal_summary()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.result_summary := NEW.status;
    RETURN NEW;
END
$$;

CREATE OR REPLACE TRIGGER bot_command_execution_terminal_summary_scrub
    BEFORE INSERT OR UPDATE OF status, result_summary
    ON bot_command_executions
    FOR EACH ROW
    WHEN (NEW.status IN ('succeeded', 'failed', 'outcome_unknown'))
    EXECUTE FUNCTION scrub_bot_command_execution_terminal_summary();

UPDATE bot_command_executions
SET result_summary = status
WHERE status IN ('succeeded', 'failed', 'outcome_unknown')
  AND result_summary IS DISTINCT FROM status;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'bot_command_executions'::regclass
          AND conname = 'chk_bot_command_executions_terminal_summary_scrubbed'
    ) THEN
        ALTER TABLE bot_command_executions
            ADD CONSTRAINT chk_bot_command_executions_terminal_summary_scrubbed
            CHECK (
                status NOT IN ('succeeded', 'failed', 'outcome_unknown')
                OR result_summary = status
            ) NOT VALID;
    END IF;
END
$$;

ALTER TABLE bot_command_executions
    VALIDATE CONSTRAINT chk_bot_command_executions_terminal_summary_scrubbed;
