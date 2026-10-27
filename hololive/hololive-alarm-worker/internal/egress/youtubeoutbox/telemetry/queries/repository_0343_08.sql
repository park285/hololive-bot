UPDATE youtube_notification_delivery_telemetry AS t
SET actual_published_at = v.actual_published_at,
    alarm_sent_at = v.alarm_sent_at,
    alarm_latency_millis = v.alarm_latency_millis,
    detected_at = v.detected_at
FROM unnest($1::bigint[], $2::timestamptz[], $3::timestamptz[], $4::bigint[], $5::timestamptz[])
    AS v(id, actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at)
WHERE t.id = v.id
