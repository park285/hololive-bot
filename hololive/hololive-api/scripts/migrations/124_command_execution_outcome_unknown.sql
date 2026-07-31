ALTER TABLE bot_command_executions
    DROP CONSTRAINT IF EXISTS chk_bot_command_executions_status_vocab,
    ADD CONSTRAINT chk_bot_command_executions_status_vocab CHECK (
        status IN ('claimed', 'succeeded', 'failed', 'outcome_unknown')
    );
