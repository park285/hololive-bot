-- claim이 같은 트랜잭션에서 잠근 종료 후보를 Go 재시도 정책이 정한 사유로 한 번에 DEAD_LETTER로 끝낸다.
-- 시각은 claim과 같은 트랜잭션 시각이다. 다른 lease가 살아 있는 행은 due 조건에서 빠져 0행이 되고,
-- 호출자가 적용 수 불일치를 오류로 보고한다.
UPDATE source_observation_queue AS queue
SET status = 'DEAD_LETTER',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = NOW(),
    last_error_code = settled.error_code,
    last_error_detail = NULL,
    updated_at = NOW()
FROM unnest($1::bigint[], $2::text[]) AS settled(observation_id, error_code)
WHERE queue.observation_id = settled.observation_id
  AND (
      (queue.status = 'PENDING' AND queue.available_at <= NOW())
      OR
      (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
  )
