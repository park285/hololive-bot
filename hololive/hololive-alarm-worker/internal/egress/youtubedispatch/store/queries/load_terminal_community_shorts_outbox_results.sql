SELECT o.id, o.kind, o.channel_id, o.content_id, o.payload::text AS payload, o.status, o.attempt_count, o.next_attempt_at, o.created_at, o.locked_at, o.sent_at, COALESCE(o.error, '') AS error,
       track.kind IS NOT NULL AS tracked,
       track.actual_published_at AS tracked_actual_published_at,
       track.alarm_sent_at AS tracked_alarm_sent_at,
       track.alarm_latency_millis AS tracked_alarm_latency_millis
FROM youtube_notification_outbox AS o
LEFT JOIN youtube_content_alarm_tracking AS track
  ON track.kind = o.kind
 AND track.content_id = o.content_id
WHERE o.id = ANY($1::bigint[])
  AND o.kind = ANY($2::text[])
  AND o.status = ANY($3::text[])
ORDER BY o.id ASC
