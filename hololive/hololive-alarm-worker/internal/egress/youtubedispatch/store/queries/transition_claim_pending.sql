-- 만료·신선도·due 판정과 새 locked_at은 문장 첫머리에서 한 번 채취한 DB 시계(clock)로 정한다. 스칼라 하위
-- 질의로 참조해 InitPlan 값이 되므로 PENDING 부분 인덱스 조건으로 쓰인다. clock은 행을 잠그기 전에 채취하므로
-- lease 만료·due 판정은 실제 잠금 시점보다 보수적이고, 새 lease도 그만큼 일찍 끝난다.
-- $1 상태, $2 lease ms, $3 신선도 ms, $4 batch 크기, $5 claim 소유자.
WITH clock AS MATERIALIZED (
    SELECT sampled.at,
           sampled.at - $2::bigint * INTERVAL '1 millisecond' AS lock_cutoff,
           sampled.at - $3::bigint * INTERVAL '1 millisecond' AS fresh_cutoff
    FROM (SELECT clock_timestamp() AS at) AS sampled
), units AS (
    SELECT delivery.id, delivery.next_attempt_at, delivery.created_at, delivery.send_request_id,
           COALESCE(request.member_ids, ARRAY[delivery.id]) AS member_ids,
           COALESCE(cardinality(request.member_ids), 1) AS member_count
    FROM youtube_notification_delivery delivery
    JOIN youtube_notification_outbox outbox ON outbox.id = delivery.outbox_id
    LEFT JOIN youtube_notification_send_request request ON request.base_id = delivery.send_request_id
    WHERE /* ready_predicate */
      AND (request.base_id IS NULL OR
           delivery.id = (SELECT min(id) FROM unnest(request.member_ids) AS ids(id)))
    ORDER BY delivery.next_attempt_at, delivery.created_at, delivery.id
    LIMIT $4
), budgeted AS (
    SELECT member_ids, send_request_id, sum(member_count) OVER (ORDER BY next_attempt_at, created_at, id) AS total
    FROM units
), request_locks AS MATERIALIZED (
    SELECT request.base_id
    FROM youtube_notification_send_request request
    WHERE request.base_id IN (SELECT send_request_id FROM budgeted WHERE total <= $4)
    ORDER BY request.base_id
    FOR UPDATE SKIP LOCKED
), claim AS (
    SELECT delivery.id
    FROM youtube_notification_delivery delivery
    WHERE delivery.id IN (SELECT unnest(member_ids) FROM budgeted WHERE total <= $4
      AND (send_request_id IS NULL OR send_request_id IN (SELECT base_id FROM request_locks)))
      AND delivery.status = $1 AND delivery.status = 'PENDING'
      AND (delivery.locked_at IS NULL OR delivery.locked_at < (SELECT lock_cutoff FROM clock))
      AND delivery.next_attempt_at <= (SELECT at FROM clock)
      AND EXISTS (
          SELECT 1 FROM youtube_notification_outbox parent
          WHERE parent.id = delivery.outbox_id AND parent.created_at >= (SELECT fresh_cutoff FROM clock)
      )
    ORDER BY delivery.id
    FOR UPDATE OF delivery SKIP LOCKED
), updated AS (
    UPDATE youtube_notification_delivery AS delivery
    SET locked_at = (SELECT at FROM clock),
        locked_by = $5::text,
        row_version = delivery.row_version + 1
    FROM claim
    WHERE delivery.id = claim.id
      AND delivery.status = $1
    RETURNING delivery.id,
              delivery.outbox_id,
              delivery.room_id,
              delivery.status,
              delivery.attempt_count,
              delivery.next_attempt_at,
              delivery.created_at,
              delivery.locked_at,
              delivery.locked_by,
              delivery.sent_at,
              COALESCE(delivery.error, '') AS error,
              delivery.row_version
)
SELECT id, outbox_id, room_id, status, attempt_count, next_attempt_at,
       created_at, locked_at, locked_by, sent_at, error, row_version
FROM updated
ORDER BY next_attempt_at, created_at, id;
