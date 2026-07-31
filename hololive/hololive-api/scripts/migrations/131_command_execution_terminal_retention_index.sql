CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_command_executions_terminal_updated
    ON bot_command_executions (updated_at ASC, id ASC)
    WHERE status IN ('succeeded', 'failed', 'outcome_unknown');
