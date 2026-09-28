SELECT delivery_id, MAX(attempt_ordinal)
FROM youtube_notification_delivery_telemetry
WHERE delivery_id = ANY($1::BIGINT[])
GROUP BY delivery_id;
