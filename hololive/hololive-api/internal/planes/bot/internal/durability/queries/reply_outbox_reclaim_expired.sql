-- Go에서 결정한 전이만 저장한다. 재시도는 잠금 이후에도 남은 지평 안에서만 허용한다.
WITH input AS (
    SELECT id, source_status, target_status, last_error, retry
    FROM unnest($1::bigint[], $2::text[], $3::text[], $4::text[], $5::boolean[])
        AS values(id, source_status, target_status, last_error, retry)
)
UPDATE bot_reply_outbox AS outbox
SET status = input.target_status,
    claim_token = NULL,
    lease_until = NULL,
    last_error = input.last_error,
    updated_at = clock_timestamp()
FROM input
WHERE outbox.id = input.id AND outbox.status = input.source_status
  AND (NOT input.retry
       OR outbox.first_attempt_at > clock_timestamp() - ($6::bigint * INTERVAL '1 millisecond'))
