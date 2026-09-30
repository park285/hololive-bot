CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_delivery_send_request
    ON youtube_notification_delivery(send_request_id) WHERE send_request_id IS NOT NULL;
