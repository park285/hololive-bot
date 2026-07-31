WITH inbox_candidate AS MATERIALIZED (
    SELECT id
    FROM bot_webhook_inbox
    WHERE status IN ('dead', 'succeeded')
      AND updated_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
    ORDER BY updated_at ASC, id ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
), deleted_inbox AS (
    DELETE FROM bot_webhook_inbox AS inbox
    USING inbox_candidate
    WHERE inbox.id = inbox_candidate.id
    RETURNING inbox.id
), command_candidate AS MATERIALIZED (
    SELECT id
    FROM bot_command_executions
    WHERE status IN ('succeeded', 'failed', 'outcome_unknown')
      AND updated_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
    ORDER BY updated_at ASC, id ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
), deleted_command AS (
    DELETE FROM bot_command_executions AS command
    USING command_candidate
    WHERE command.id = command_candidate.id
    RETURNING command.id
), outbox_candidate AS MATERIALIZED (
    SELECT id
    FROM bot_reply_outbox
    WHERE (
        status IN ('handoff_completed', 'dead', 'permanent_conflict')
        AND updated_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
    ) OR (
        status = 'manual_review'
        AND updated_at < clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond')
    )
    ORDER BY updated_at ASC, id ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
), deleted_outbox AS (
    DELETE FROM bot_reply_outbox AS outbox
    USING outbox_candidate
    WHERE outbox.id = outbox_candidate.id
    RETURNING outbox.id
)
SELECT (SELECT count(id) FROM deleted_inbox)::bigint,
       (SELECT count(id) FROM deleted_command)::bigint,
       (SELECT count(id) FROM deleted_outbox)::bigint;
