WITH expired AS (
    SELECT request.base_id
    FROM youtube_notification_send_request request
    WHERE request.created_at < $1
      AND NOT EXISTS (
          SELECT 1 FROM youtube_notification_delivery delivery
          WHERE delivery.send_request_id = request.base_id
      )
    ORDER BY request.created_at, request.base_id
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
DELETE FROM youtube_notification_send_request request
USING expired WHERE request.base_id = expired.base_id;
