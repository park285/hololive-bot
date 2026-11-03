-- $1..$5는 같은 길이의 배열이며 호출자가 (kind, content_id)를 중복 없이 넘긴다.
-- 같은 (kind, content_id)를 가진 tracking 행은 모두 같은 분류 값으로 갱신된다.
UPDATE youtube_content_alarm_tracking AS track
SET latency_classification_status = v.latency_classification_status,
    delay_source = v.delay_source,
    internal_delay_cause = v.internal_delay_cause,
    updated_at = $6
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
    AS v(kind, content_id, latency_classification_status, delay_source, internal_delay_cause)
WHERE track.kind = v.kind
  AND track.content_id = v.content_id
