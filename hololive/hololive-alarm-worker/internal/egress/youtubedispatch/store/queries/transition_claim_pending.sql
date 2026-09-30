WITH units AS (
    SELECT delivery.id, delivery.next_attempt_at, delivery.created_at, delivery.send_request_id,
           COALESCE(request.member_ids, ARRAY[delivery.id]) AS member_ids,
           COALESCE(cardinality(request.member_ids), 1) AS member_count
    FROM youtube_notification_delivery delivery
    JOIN youtube_notification_outbox outbox ON outbox.id = delivery.outbox_id
    LEFT JOIN youtube_notification_send_request request ON request.base_id = delivery.send_request_id
    WHERE delivery.status = $1 AND delivery.status = 'PENDING'
      AND COALESCE(cardinality(request.member_ids), 1) <= $5
      AND (delivery.locked_at IS NULL OR delivery.locked_at < $2)
      AND delivery.next_attempt_at <= $3 AND outbox.created_at >= $4
      AND (request.base_id IS NULL OR (
          delivery.id = (SELECT min(id) FROM unnest(request.member_ids) AS ids(id))
          AND NOT EXISTS (
              SELECT 1 FROM unnest(request.member_ids) AS ids(id)
              LEFT JOIN youtube_notification_delivery member ON member.id = ids.id
              LEFT JOIN youtube_notification_outbox parent ON parent.id = member.outbox_id
              WHERE member.id IS NULL OR member.status <> 'PENDING'
                 OR member.next_attempt_at > $3 OR parent.created_at < $4
                 OR (member.locked_at IS NOT NULL AND member.locked_at >= $2)
          )
      ))
    ORDER BY delivery.next_attempt_at, delivery.created_at, delivery.id
    LIMIT $5
), budgeted AS (
    SELECT member_ids, send_request_id, sum(member_count) OVER (ORDER BY next_attempt_at, created_at, id) AS total
    FROM units
), request_locks AS MATERIALIZED (
    SELECT request.base_id
    FROM youtube_notification_send_request request
    WHERE request.base_id IN (SELECT send_request_id FROM budgeted WHERE total <= $5)
    ORDER BY request.base_id
    FOR UPDATE SKIP LOCKED
), claim AS (
    SELECT delivery.id
    FROM youtube_notification_delivery delivery
    WHERE delivery.id IN (SELECT unnest(member_ids) FROM budgeted WHERE total <= $5
      AND (send_request_id IS NULL OR send_request_id IN (SELECT base_id FROM request_locks)))
      AND delivery.status = $1 AND delivery.status = 'PENDING'
      AND (delivery.locked_at IS NULL OR delivery.locked_at < $2)
      AND delivery.next_attempt_at <= $3
      AND EXISTS (SELECT 1 FROM youtube_notification_outbox parent WHERE parent.id=delivery.outbox_id AND parent.created_at >= $4)
    ORDER BY delivery.id
    FOR UPDATE OF delivery SKIP LOCKED
), updated AS (
    UPDATE youtube_notification_delivery AS delivery
    SET locked_at = $6,
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
              delivery.sent_at,
              COALESCE(delivery.error, '') AS error,
              delivery.row_version
)
SELECT id, outbox_id, room_id, status, attempt_count, next_attempt_at,
       created_at, locked_at, sent_at, error, row_version
FROM updated
ORDER BY next_attempt_at, created_at, id;
