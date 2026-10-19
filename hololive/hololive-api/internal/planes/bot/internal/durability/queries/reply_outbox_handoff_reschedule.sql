-- 다음 확인은 지평(accepted_at + horizon)을 넘기지 않는다. 지평 시각의 확인이 마지막 확인이다.
UPDATE bot_reply_outbox
SET handoff_next_check_at = LEAST(
        clock_timestamp() + ($4::bigint * INTERVAL '1 millisecond'),
        accepted_at + ($5::bigint * INTERVAL '1 millisecond')
    )
WHERE id = $1
  AND status = 'accepted'
  AND claim_token IS NULL
  AND handoff_next_check_at = $2
  AND iris_request_id = $3
RETURNING GREATEST(ceil(EXTRACT(EPOCH FROM (handoff_next_check_at - clock_timestamp())) * 1000), 0)::bigint
