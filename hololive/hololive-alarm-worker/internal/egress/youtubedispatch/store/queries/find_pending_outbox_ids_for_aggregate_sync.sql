SELECT o.id
FROM youtube_notification_outbox AS o
WHERE o.status = 'PENDING'
  AND EXISTS (
      SELECT 1
      FROM youtube_notification_delivery AS d
      WHERE d.outbox_id = o.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_notification_delivery AS d
      WHERE d.outbox_id = o.id
        AND d.status IN ('PENDING', 'SENDING')
  )
ORDER BY o.id ASC
LIMIT $1
