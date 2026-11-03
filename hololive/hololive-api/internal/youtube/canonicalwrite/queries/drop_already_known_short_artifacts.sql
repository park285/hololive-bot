SELECT v.video_id
FROM youtube_videos v
WHERE v.video_id = ANY($1::text[])
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_notification_outbox o
      WHERE o.kind = 'NEW_SHORT'
        AND o.content_id IN (v.video_id, CONCAT('short:', v.video_id))
  )
