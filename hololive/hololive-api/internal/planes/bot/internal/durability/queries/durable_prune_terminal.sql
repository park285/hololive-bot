-- 보존 cutoff는 STABLE인 statement_timestamp()로 계산해 index cond가 되게 한다. 이 문장은 단일
-- autocommit 문장이라 행별 clock_timestamp()와의 차이는 문장 실행 시간 이하다. 각 OR arm은 terminal
-- 부분 인덱스 술어(inbox·command terminal, outbox terminal·discarded·manual_review)와 글자 그대로 맞춰야
-- 플래너가 그 인덱스로 만료 행만 읽는다. 상태를 추가하거나 인덱스 술어를 바꾸면 이 문장도 함께 바꾼다.
WITH inbox_candidate AS MATERIALIZED (
    SELECT id
    FROM bot_webhook_inbox
    WHERE status IN ('dead', 'succeeded')
      AND updated_at < statement_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
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
      AND updated_at < statement_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
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
        AND updated_at < statement_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
    ) OR (
        status = 'discarded'
        AND updated_at < statement_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
    ) OR (
        status = 'manual_review'
        AND updated_at < statement_timestamp() - ($2::bigint * INTERVAL '1 millisecond')
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
