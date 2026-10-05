-- $1/$2는 같은 길이의 (kind, content_id) 쌍 배열이다. 쌍 단위로 비교해 kind×content_id 교차 조합을 읽지 않는다.
SELECT kind,
    content_id,
    COALESCE(canonical_content_id, '') AS canonical_content_id,
    channel_id,
    actual_published_at,
    detected_at,
    alarm_sent_at,
    alarm_latency_millis,
    alarm_latency_exceeded,
    delivery_status,
    COALESCE(latency_classification_status, '') AS latency_classification_status,
    COALESCE(delay_source, '') AS delay_source,
    COALESCE(internal_delay_cause, '') AS internal_delay_cause,
    created_at,
    updated_at
FROM youtube_content_alarm_tracking
WHERE (kind, content_id) IN (
    SELECT input.kind, input.content_id
    FROM unnest($1::text[], $2::text[]) AS input(kind, content_id)
)
