-- Iris failed는 pre-dispatch 소진 또는 거절이다. 같은 행을 재발송 큐로 되돌리면 다음 claim의 reissue
-- ladder가 409 CLIENT_REQUEST_ID_FAILED를 받고 다음 세대로 보낸다.
UPDATE bot_reply_outbox
SET status = $4,
    handoff_next_check_at = NULL,
    last_error = $5,
    available_at = clock_timestamp() + ($6::bigint * INTERVAL '1 millisecond'),
    updated_at = clock_timestamp()
WHERE id = $1
  AND status = 'accepted'
  AND claim_token IS NULL
  AND handoff_next_check_at = $2
  AND iris_request_id = $3
