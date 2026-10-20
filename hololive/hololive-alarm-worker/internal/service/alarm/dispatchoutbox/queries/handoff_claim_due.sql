-- 확인 시각이 된 send unit을 SKIP LOCKED로 $1개까지 고르고 handoff_next_check_at을 clock + $2 밀리초로 밀어 soft
-- claim한다. 밀어 둔 값이 이후 적용 문장의 CAS 토큰이다. 발송 lease는 잡지 않는다. 경과는 같은 clock에서 sent 멤버의
-- 가장 이른 sent_at(admission)까지이고, sent 멤버가 없으면 NULL이다. attempt_count는 sent 멤버의 최댓값이다.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
), due AS MATERIALIZED (
    SELECT u.id
    FROM alarm_dispatch_send_units u
    WHERE u.handoff_next_check_at <= (SELECT clock.now FROM clock)
    ORDER BY u.handoff_next_check_at ASC, u.id ASC
    LIMIT $1
    FOR UPDATE OF u SKIP LOCKED
)
UPDATE alarm_dispatch_send_units u
SET handoff_next_check_at = clock.now + ($2::BIGINT * INTERVAL '1 millisecond')
FROM due
CROSS JOIN clock
CROSS JOIN LATERAL (
    SELECT floor(EXTRACT(EPOCH FROM (clock.now - min(d.sent_at))) * 1000)::BIGINT AS elapsed_ms,
           COALESCE(max(d.attempt_count), 0) AS attempt_count,
           COALESCE(array_agg(claim_key.value ORDER BY d.id, claim_key.ordinality)
               FILTER (WHERE claim_key.value IS NOT NULL), '{}') AS claim_keys
    FROM alarm_dispatch_deliveries d
    LEFT JOIN LATERAL unnest(d.claim_keys) WITH ORDINALITY AS claim_key(value, ordinality) ON TRUE
    WHERE d.send_unit_id = due.id AND d.status = 'sent'
) AS members
WHERE u.id = due.id
RETURNING u.id, u.handoff_request_id, u.client_request_id, u.base_client_request_id, u.request_generation,
          u.request_delivery_ids, u.handoff_next_check_at, members.elapsed_ms, members.attempt_count, members.claim_keys
