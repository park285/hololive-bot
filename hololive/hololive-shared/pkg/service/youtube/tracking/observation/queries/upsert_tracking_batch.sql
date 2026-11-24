INSERT INTO youtube_content_alarm_tracking
	(kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, created_at, updated_at)
SELECT kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status, $11, $11
FROM unnest(
	$1::text[],
	$2::text[],
	$3::text[],
	$4::text[],
	$5::timestamptz[],
	$6::timestamptz[],
	$7::timestamptz[],
	$8::bigint[],
	$9::boolean[],
	$10::text[]
) AS t(kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, alarm_latency_millis, alarm_latency_exceeded, delivery_status)
ON CONFLICT (kind, canonical_content_id) DO UPDATE
SET channel_id = EXCLUDED.channel_id,
    actual_published_at = COALESCE(youtube_content_alarm_tracking.actual_published_at, EXCLUDED.actual_published_at),
    detected_at = CASE
        WHEN EXCLUDED.detected_at < youtube_content_alarm_tracking.detected_at THEN EXCLUDED.detected_at
        ELSE youtube_content_alarm_tracking.detected_at
    END,
    alarm_sent_at = LEAST(youtube_content_alarm_tracking.alarm_sent_at, EXCLUDED.alarm_sent_at),
    alarm_latency_millis = CAST(ROUND(EXTRACT(EPOCH FROM (
        LEAST(youtube_content_alarm_tracking.alarm_sent_at, EXCLUDED.alarm_sent_at)
        - COALESCE(youtube_content_alarm_tracking.actual_published_at, EXCLUDED.actual_published_at)
    )) * 1000) AS BIGINT),
    alarm_latency_exceeded = CAST(ROUND(EXTRACT(EPOCH FROM (
        LEAST(youtube_content_alarm_tracking.alarm_sent_at, EXCLUDED.alarm_sent_at)
        - COALESCE(youtube_content_alarm_tracking.actual_published_at, EXCLUDED.actual_published_at)
    )) * 1000) AS BIGINT) > $12::bigint,
    delivery_status = CASE
        WHEN LEAST(youtube_content_alarm_tracking.alarm_sent_at, EXCLUDED.alarm_sent_at) IS NULL THEN 'PENDING'
        ELSE 'SENT'
    END,
    updated_at = EXCLUDED.updated_at
