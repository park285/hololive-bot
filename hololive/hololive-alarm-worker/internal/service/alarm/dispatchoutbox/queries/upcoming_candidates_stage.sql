WITH staged AS (
    INSERT INTO alarm_upcoming_candidates
        (dedupe_key, event_key, payload_hash, channel_id, stream_id, room_id,
         scheduled_at, notification, selected_at, checked_at)
    SELECT dedupe_key, event_key, payload_hash, $2, stream_id, room_id,
           scheduled_at, notification, $3, $3
    FROM jsonb_to_recordset($1::jsonb) AS x(
        dedupe_key TEXT, event_key TEXT, payload_hash TEXT, stream_id TEXT,
        room_id TEXT, scheduled_at TIMESTAMPTZ, notification JSONB)
    ON CONFLICT (dedupe_key) DO NOTHING
)
INSERT INTO alarm_upcoming_checkpoints (channel_id, evaluated_at) VALUES ($2, $3)
ON CONFLICT (channel_id) DO UPDATE SET evaluated_at = EXCLUDED.evaluated_at
WHERE alarm_upcoming_checkpoints.evaluated_at < EXCLUDED.evaluated_at
