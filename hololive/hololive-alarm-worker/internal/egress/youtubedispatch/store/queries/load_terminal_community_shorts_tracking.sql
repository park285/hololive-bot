SELECT track.kind, track.canonical_content_id, track.actual_published_at, track.alarm_sent_at, track.alarm_latency_millis
FROM youtube_content_alarm_tracking AS track
JOIN unnest($1::text[], $2::text[]) AS input(kind, canonical_content_id)
  ON track.kind = input.kind
 AND track.canonical_content_id = input.canonical_content_id
