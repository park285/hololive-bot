UPDATE youtube_content_alarm_tracking AS track
SET latency_classification_status = v.latency_classification_status,
    delay_source = v.delay_source,
    internal_delay_cause = v.internal_delay_cause,
    updated_at = $6
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
    AS v(kind, content_id, latency_classification_status, delay_source, internal_delay_cause)
WHERE track.kind = v.kind
  AND track.content_id = v.content_id
