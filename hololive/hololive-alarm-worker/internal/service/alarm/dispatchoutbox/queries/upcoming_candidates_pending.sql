WITH selected AS MATERIALIZED (
    SELECT c.dedupe_key,
        CASE
            WHEN e.payload_hash IS NOT NULL AND e.payload_hash <> c.payload_hash THEN 'rejected_collision'
            WHEN d.status IN ('pending', 'leased', 'retry', 'sending', 'sent') THEN 'accepted'
            WHEN d.status IS NOT NULL THEN 'rejected_terminal'
            WHEN c.scheduled_at <= $1 THEN 'expired'
            WHEN live.is_premiere IS TRUE
                OR (live.status_observed_at > c.selected_at AND live.status IN ('LIVE', 'ENDED')) THEN 'stream_ended'
            WHEN live.schedule_observed_at > c.selected_at AND live.scheduled_start_time IS NOT NULL
                AND live.scheduled_start_time <> c.scheduled_at THEN 'schedule_changed'
            ELSE 'pending'
        END AS outcome
    FROM alarm_upcoming_candidates c
    LEFT JOIN alarm_dispatch_events e ON e.event_key = c.event_key
    LEFT JOIN alarm_dispatch_deliveries d ON d.dedupe_key = c.dedupe_key
    LEFT JOIN youtube_live_sessions live ON live.video_id = c.stream_id AND live.channel_id = c.channel_id
    WHERE c.outcome = 'pending'
    ORDER BY c.checked_at, c.dedupe_key
    LIMIT $2
    FOR UPDATE OF c SKIP LOCKED
), updated AS (
    UPDATE alarm_upcoming_candidates c
    SET outcome = s.outcome, checked_at = $1,
        terminal_at = CASE WHEN s.outcome = 'pending' THEN NULL ELSE $1 END
    FROM selected s WHERE c.dedupe_key = s.dedupe_key
    RETURNING c.dedupe_key, c.channel_id, c.notification, c.outcome
)
SELECT dedupe_key, channel_id, notification, outcome FROM updated WHERE outcome = 'pending'
