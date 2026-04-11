-- 050: 커뮤니티/쇼츠 발송 telemetry에 관찰 구간 식별자와 기준 시각 추가
ALTER TABLE youtube_notification_delivery_telemetry
    ADD COLUMN IF NOT EXISTS actual_published_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS detected_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS observation_status VARCHAR(40) DEFAULT 'unclassified',
    ADD COLUMN IF NOT EXISTS observation_runtime_name VARCHAR(50),
    ADD COLUMN IF NOT EXISTS observation_bigbang_cutover_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS observation_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS observation_ended_at TIMESTAMPTZ;

WITH observation_match AS (
    SELECT
        t.id AS telemetry_id,
        track.actual_published_at AS actual_published_at,
        track.detected_at AS detected_at,
        obs.runtime_name AS observation_runtime_name,
        obs.bigbang_cutover_at AS observation_bigbang_cutover_at,
        obs.observation_started_at AS observation_started_at,
        obs.observation_ended_at AS observation_ended_at,
        CASE
            WHEN track.kind IS NULL THEN 'tracking_not_found'
            WHEN track.actual_published_at IS NULL THEN 'missing_actual_published_at'
            WHEN obs.runtime_name IS NULL THEN 'outside_observation_window'
            ELSE 'matched'
        END AS observation_status
    FROM youtube_notification_delivery_telemetry AS t
    LEFT JOIN youtube_notification_outbox AS o
        ON o.id = t.outbox_id
    LEFT JOIN youtube_content_alarm_tracking AS track
        ON track.kind = o.kind
       AND track.content_id = o.content_id
    LEFT JOIN LATERAL (
        SELECT
            w.runtime_name,
            w.bigbang_cutover_at,
            w.observation_started_at,
            w.observation_ended_at
        FROM youtube_community_shorts_observation_windows AS w
        WHERE track.actual_published_at IS NOT NULL
          AND track.actual_published_at >= w.observation_started_at
          AND track.actual_published_at < w.observation_ended_at
        ORDER BY w.observation_started_at DESC, w.bigbang_cutover_at DESC
        LIMIT 1
    ) AS obs ON TRUE
)
UPDATE youtube_notification_delivery_telemetry AS t
SET actual_published_at = m.actual_published_at,
    detected_at = m.detected_at,
    observation_status = m.observation_status,
    observation_runtime_name = m.observation_runtime_name,
    observation_bigbang_cutover_at = m.observation_bigbang_cutover_at,
    observation_started_at = m.observation_started_at,
    observation_ended_at = m.observation_ended_at
FROM observation_match AS m
WHERE m.telemetry_id = t.id;

UPDATE youtube_notification_delivery_telemetry
SET observation_status = 'unclassified'
WHERE COALESCE(BTRIM(observation_status), '') = '';

ALTER TABLE youtube_notification_delivery_telemetry
    ALTER COLUMN observation_status SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ydt_observation_window_event
    ON youtube_notification_delivery_telemetry(observation_runtime_name, observation_bigbang_cutover_at, event_at);

CREATE INDEX IF NOT EXISTS idx_ydt_observation_status_event
    ON youtube_notification_delivery_telemetry(observation_status, event_at);

COMMENT ON COLUMN youtube_notification_delivery_telemetry.actual_published_at IS '관찰 구간 판정에 사용한 실제 유튜브 게시 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_notification_delivery_telemetry.detected_at IS '관찰 구간 판정 시 참고한 최초 감지 시각 (UTC canonical)';
COMMENT ON COLUMN youtube_notification_delivery_telemetry.observation_status IS '관찰 구간 매칭 상태: matched/outside_observation_window/missing_actual_published_at/tracking_not_found/unclassified';
COMMENT ON COLUMN youtube_notification_delivery_telemetry.observation_runtime_name IS '매칭된 24시간 관찰 구간의 runtime 이름';
COMMENT ON COLUMN youtube_notification_delivery_telemetry.observation_bigbang_cutover_at IS '매칭된 24시간 관찰 구간의 big-bang cutover 기준 시각';
COMMENT ON COLUMN youtube_notification_delivery_telemetry.observation_started_at IS '매칭된 24시간 관찰 구간 시작 시각';
COMMENT ON COLUMN youtube_notification_delivery_telemetry.observation_ended_at IS '매칭된 24시간 관찰 구간 종료 시각';
