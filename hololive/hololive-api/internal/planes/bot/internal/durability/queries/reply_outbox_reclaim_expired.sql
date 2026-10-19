-- Go에서 결정한 전이만 저장한다. 대상은 만료 lease를 가진 submitting뿐이고, 재시도는 잠금 이후에도 남은 지평 안에서만 허용한다.
WITH input AS (
    SELECT id, target_status, last_error, retry
    FROM unnest($1::bigint[], $2::text[], $3::text[], $4::boolean[])
        AS values(id, target_status, last_error, retry)
)
UPDATE bot_reply_outbox AS outbox
SET status = input.target_status,
    claim_token = NULL,
    lease_until = NULL,
    last_error = input.last_error,
    updated_at = clock_timestamp()
FROM input
WHERE outbox.id = input.id AND outbox.status = 'submitting'
  AND (NOT input.retry
       OR outbox.first_attempt_at > clock_timestamp() - ($5::bigint * INTERVAL '1 millisecond'))
