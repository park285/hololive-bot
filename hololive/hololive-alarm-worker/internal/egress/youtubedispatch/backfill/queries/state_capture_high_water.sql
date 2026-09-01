SELECT
    COALESCE((SELECT MAX(id) FROM youtube_notification_delivery), 0),
    COALESCE((SELECT MAX(id) FROM youtube_notification_outbox), 0),
    clock_timestamp();
