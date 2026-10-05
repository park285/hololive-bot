WITH selected AS MATERIALIZED (
    SELECT c.dedupe_key, c.channel_id, c.notification, c.payload_hash,
           c.event_key, c.stream_id, c.scheduled_at, c.selected_at, c.checked_at
    FROM alarm_upcoming_candidates c
    WHERE c.outcome = 'pending'
    ORDER BY c.checked_at, c.dedupe_key
    LIMIT $1
    FOR UPDATE OF c SKIP LOCKED
)
SELECT c.dedupe_key, c.channel_id, c.notification,
       c.payload_hash, e.payload_hash, d.status, c.scheduled_at, c.selected_at,
       live.status, live.is_premiere, live.status_observed_at,
       live.schedule_observed_at, live.scheduled_start_time
FROM selected c
LEFT JOIN alarm_dispatch_events e ON e.event_key = c.event_key
LEFT JOIN alarm_dispatch_deliveries d ON d.dedupe_key = c.dedupe_key
LEFT JOIN youtube_live_sessions live ON live.video_id = c.stream_id AND live.channel_id = c.channel_id
ORDER BY c.checked_at, c.dedupe_key
