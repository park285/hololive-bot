SELECT COUNT(delivery.id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(delivery.created_at))), 0), 0)
FROM youtube_notification_delivery AS delivery
JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
LEFT JOIN youtube_notification_send_request AS request ON request.base_id = delivery.send_request_id
WHERE delivery.status = 'PENDING'
  AND delivery.next_attempt_at <= clock_timestamp()
  AND (
      delivery.locked_at IS NULL
      OR delivery.locked_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
  )
  AND outbox.created_at >= clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond')
  AND COALESCE(cardinality(request.member_ids), 1) <= $3
  AND (request.base_id IS NULL OR NOT EXISTS (
      SELECT 1 FROM unnest(request.member_ids) AS ids(id)
      LEFT JOIN youtube_notification_delivery AS member ON member.id = ids.id
      LEFT JOIN youtube_notification_outbox AS parent ON parent.id = member.outbox_id
      WHERE member.id IS NULL OR member.status <> 'PENDING'
         OR member.next_attempt_at > clock_timestamp()
         OR parent.created_at < clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond')
         OR (member.locked_at IS NOT NULL
             AND member.locked_at >= clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond'))
  ))
