-- accepted는 Iris가 이미 수리한 뒤라 재발송하면 중복 발화다. lease만 잃은 것이므로 handoff로 흡수한다.
-- submitting은 발송 여부가 미확정이라 저장 payload와 같은 client_request_id로 다시 큐에 올린다.
WITH candidate AS MATERIALIZED (
    SELECT id, status
    FROM bot_reply_outbox
    WHERE status IN ('submitting', 'accepted')
      AND lease_until <= clock_timestamp()
    ORDER BY lease_until ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
), reclaimed AS (
    UPDATE bot_reply_outbox AS outbox
    SET status = CASE WHEN candidate.status = 'accepted' THEN 'handoff_completed' ELSE 'pending' END,
        claim_token = NULL,
        lease_until = NULL,
        payload = CASE WHEN candidate.status = 'accepted' THEN NULL ELSE outbox.payload END,
        last_error = 'submit lease expired',
        updated_at = clock_timestamp()
    FROM candidate
    WHERE outbox.id = candidate.id
    RETURNING outbox.status
)
SELECT
    count(status) FILTER (WHERE status = 'pending')::bigint AS requeued,
    count(status) FILTER (WHERE status = 'handoff_completed')::bigint AS absorbed
FROM reclaimed
