)
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_notification_outbox o
      WHERE o.kind = 'NEW_SHORT'
        AND o.content_id IN (v.video_id, CONCAT('short:', v.video_id))
  )
