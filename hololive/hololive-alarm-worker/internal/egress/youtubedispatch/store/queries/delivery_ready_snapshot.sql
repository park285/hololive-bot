SELECT COUNT(delivery.id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM ($3::timestamptz - MIN(
           CASE WHEN delivery.locked_at IS NULL THEN delivery.next_attempt_at
                ELSE GREATEST(delivery.next_attempt_at, delivery.locked_at + ($3::timestamptz - $2::timestamptz)) END
       ))), 0), 0)
FROM youtube_notification_delivery delivery
JOIN youtube_notification_outbox outbox ON outbox.id = delivery.outbox_id
LEFT JOIN youtube_notification_send_request request ON request.base_id = delivery.send_request_id
WHERE /* ready_predicate */
