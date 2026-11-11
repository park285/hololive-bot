SELECT COUNT(expired.id)
FROM (
    SELECT delivery.id
    FROM youtube_notification_delivery AS delivery
    JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
    WHERE delivery.status = 'PENDING'
      AND outbox.created_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
    ORDER BY delivery.id
    LIMIT $2
) AS expired
