WITH picked AS (
    SELECT id
    FROM youtube_notification_delivery_telemetry
    WHERE logged_at IS NULL
      AND next_attempt_at <= $1
      AND (locked_at IS NULL OR locked_at < $2)
    ORDER BY event_at ASC, id ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
UPDATE youtube_notification_delivery_telemetry AS telemetry
SET locked_at = $1
FROM picked
WHERE telemetry.id = picked.id
RETURNING telemetry.id, telemetry.delivery_id, telemetry.attempt_ordinal, telemetry.outbox_id,
          telemetry.channel_id, telemetry.content_id, telemetry.post_id, telemetry.room_id, telemetry.alarm_type,
          telemetry.actual_published_at, telemetry.alarm_sent_at, telemetry.alarm_latency_millis, telemetry.detected_at,
          telemetry.dedupe_key, telemetry.delivery_path, telemetry.delivery_mode, telemetry.send_result,
          telemetry.failure_reason, telemetry.attempt_started_at, telemetry.attempt_finished_at, telemetry.event_at,
          telemetry.next_attempt_at, telemetry.created_at, telemetry.locked_at, telemetry.logged_at, telemetry.error
