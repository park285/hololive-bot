-- 확인 시각이 된 확인 대기 SENT 행을 SKIP LOCKED로 $1개까지 고르고 handoff_next_check_at을 clock + $2 밀리초로 밀어
-- soft claim한다. 밀어 둔 값이 이후 적용 문장의 CAS 토큰이다. 발송 lease는 잡지 않는다. 경과는 같은 clock에서
-- admission(sent_at)까지다. 고정 요청 세대와 attempt_count는 재발급 세대·시도 예산 판정에 쓴다.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
), due AS MATERIALIZED (
    SELECT o.id
    FROM notification_delivery_outbox o
    WHERE o.handoff_next_check_at <= (SELECT clock.now FROM clock)
      AND o.status = 'SENT'
      AND o.handoff_checked_at IS NULL
    ORDER BY o.handoff_next_check_at ASC, o.id ASC
    LIMIT $1
    FOR UPDATE OF o SKIP LOCKED
)
UPDATE notification_delivery_outbox o
SET handoff_next_check_at = clock.now + ($2::BIGINT * INTERVAL '1 millisecond')
FROM due
CROSS JOIN clock
WHERE o.id = due.id
RETURNING o.id, o.handoff_request_id, (o.payload->'request'->>'generation')::INT AS generation, o.attempt_count,
          o.handoff_next_check_at, floor(EXTRACT(EPOCH FROM (clock.now - o.sent_at)) * 1000)::BIGINT AS elapsed_ms
