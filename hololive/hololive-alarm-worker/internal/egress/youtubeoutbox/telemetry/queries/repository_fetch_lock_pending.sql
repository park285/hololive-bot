-- 기록 대상 선택과 lease(locked_at) 획득을 한 문장으로 묶는다. FOR UPDATE SKIP LOCKED는 다른
-- 인스턴스가 잡고 있는 행을 기다리지 않고 건너뛰고, 잠근 뒤의 재검사는 그사이 lease가 갱신된 행을 뺀다.
-- UPDATE ... RETURNING은 순서를 보장하지 않으므로 호출자가 (event_at, id) 순으로 다시 정렬한다.
-- RETURNING 열 순서는 repository_scan.go의 scanTelemetryRow와 같아야 한다.
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
