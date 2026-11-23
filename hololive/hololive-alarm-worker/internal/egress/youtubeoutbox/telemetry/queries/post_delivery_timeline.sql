SELECT COALESCE(MAX(o.id), 0) AS outbox_id,
       track.kind AS outbox_kind,
       CASE track.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' WHEN 'NEW_SHORT' THEN 'SHORTS' ELSE 'LIVE' END AS alarm_type,
       track.channel_id AS channel_id,
       COALESCE(MAX(NULLIF(t.post_id, '')), track.content_id) AS post_id,
       track.content_id AS content_id,
       track.actual_published_at AS actual_published_at,
       track.detected_at AS detected_at,
       MIN(o.created_at) AS queue_enqueued_at,
       MIN(t.attempt_started_at) AS first_attempt_started_at,
       MAX(t.attempt_started_at) AS last_attempt_started_at,
       MIN(COALESCE(t.attempt_finished_at, t.event_at)) AS first_attempt_finished_at,
       MAX(COALESCE(t.attempt_finished_at, t.event_at)) AS last_attempt_finished_at,
       track.alarm_sent_at AS alarm_sent_at,
       MIN(CASE WHEN t.send_result = 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS first_success_at,
       MAX(CASE WHEN t.send_result = 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS last_success_at,
       MAX(CASE WHEN t.send_result <> 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS last_failure_at,
       MAX(CASE WHEN t.send_result <> 'success' AND t.next_attempt_at > COALESCE(t.attempt_finished_at, t.event_at) THEN t.next_attempt_at END) AS next_retry_at,
       COALESCE(SUM(CASE WHEN t.send_result = 'success' THEN 1 ELSE 0 END), 0) AS success_send_count,
       COALESCE(SUM(CASE WHEN t.send_result <> 'success' THEN 1 ELSE 0 END), 0) AS failed_attempt_count,
       COALESCE(MAX(t.attempt_ordinal), 0) AS max_attempt_ordinal,
       track.alarm_latency_millis AS alarm_latency_millis,
       track.alarm_latency_exceeded AS alarm_latency_exceeded,
       COALESCE(track.latency_classification_status, '') AS latency_classification_status,
       COALESCE(track.delay_source, '') AS delay_source,
       COALESCE(track.internal_delay_cause, '') AS internal_delay_cause
FROM youtube_content_alarm_tracking AS track
LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id
LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id
WHERE track.kind = ANY($1::text[])
  AND /* fragment:filter */
GROUP BY track.kind, track.channel_id, track.content_id, track.actual_published_at, track.detected_at, track.alarm_sent_at, track.alarm_latency_millis, track.alarm_latency_exceeded, track.latency_classification_status, track.delay_source, track.internal_delay_cause
ORDER BY COALESCE(track.alarm_sent_at, MAX(COALESCE(t.attempt_finished_at, t.event_at)), track.actual_published_at, track.detected_at) DESC,
         track.content_id ASC
