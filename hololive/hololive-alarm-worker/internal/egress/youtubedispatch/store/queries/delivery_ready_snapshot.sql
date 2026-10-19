-- 대기 시간과 claim 가능성은 claim SQL과 같은 DB 시계 표본(clock)으로 판정한다.
-- $1 상태, $2 lease ms, $3 신선도 ms, $4 batch 크기.
WITH clock AS MATERIALIZED (
    SELECT sampled.at,
           sampled.at - $2::bigint * INTERVAL '1 millisecond' AS lock_cutoff,
           sampled.at - $3::bigint * INTERVAL '1 millisecond' AS fresh_cutoff
    FROM (SELECT clock_timestamp() AS at) AS sampled
)
SELECT COUNT(delivery.id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM ((SELECT at FROM clock) - MIN(
           CASE WHEN delivery.locked_at IS NULL THEN delivery.next_attempt_at
                ELSE GREATEST(delivery.next_attempt_at, delivery.locked_at + $2::bigint * INTERVAL '1 millisecond') END
       ))), 0), 0)
FROM youtube_notification_delivery delivery
JOIN youtube_notification_outbox outbox ON outbox.id = delivery.outbox_id
LEFT JOIN youtube_notification_send_request request ON request.base_id = delivery.send_request_id
WHERE /* ready_predicate */
