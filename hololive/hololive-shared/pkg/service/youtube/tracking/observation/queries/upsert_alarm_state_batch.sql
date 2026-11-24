INSERT INTO youtube_community_shorts_alarm_states
	(kind, post_id, content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, delivery_status, created_at, updated_at)
SELECT kind, post_id, content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, delivery_status, $9, $9
FROM unnest(
	$1::text[],
	$2::text[],
	$3::text[],
	$4::text[],
	$5::timestamptz[],
	$6::timestamptz[],
	$7::timestamptz[],
	$8::text[]
) AS t(kind, post_id, content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, delivery_status)
ON CONFLICT (kind, post_id) DO UPDATE
SET content_id = EXCLUDED.content_id,
    channel_id = EXCLUDED.channel_id,
    actual_published_at = COALESCE(youtube_community_shorts_alarm_states.actual_published_at, EXCLUDED.actual_published_at),
    detected_at = CASE
        WHEN EXCLUDED.detected_at < youtube_community_shorts_alarm_states.detected_at THEN EXCLUDED.detected_at
        ELSE youtube_community_shorts_alarm_states.detected_at
    END,
    alarm_sent_at = LEAST(youtube_community_shorts_alarm_states.alarm_sent_at, EXCLUDED.alarm_sent_at),
    delivery_status = CASE
        WHEN LEAST(youtube_community_shorts_alarm_states.alarm_sent_at, EXCLUDED.alarm_sent_at) IS NULL THEN 'DETECTED'
        ELSE 'SENT'
    END,
    updated_at = EXCLUDED.updated_at
