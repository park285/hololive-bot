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
          u.request_delivery_ids, u.handoff_next_check_at, u.xmin, members.elapsed_ms, members.attempt_count, members.claim_keys
