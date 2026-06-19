-- 069: keep observation_runtime_name aligned with the Go domain string invariant.
UPDATE youtube_notification_delivery_telemetry
SET observation_runtime_name = ''
WHERE observation_runtime_name IS NULL;

ALTER TABLE youtube_notification_delivery_telemetry
    ALTER COLUMN observation_runtime_name SET DEFAULT '',
    ALTER COLUMN observation_runtime_name SET NOT NULL;
