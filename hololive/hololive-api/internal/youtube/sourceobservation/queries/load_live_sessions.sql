SELECT video_id, channel_id, status, COALESCE(title, '') AS title, topic_id, thumbnail_url,
       scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at,
       is_premiere, lifecycle_origin, status_observed_at, schedule_observed_at, title_observed_at
FROM youtube_live_sessions
WHERE video_id = ANY($2::text[])
   OR (channel_id = ANY($1::text[])
       AND (status IN ('UPCOMING', 'LIVE')
            OR video_id IN (SELECT head.video_id
                            FROM youtube_live_reconciliation_heads AS head
                            WHERE head.next_end_check_at IS NOT NULL)))
ORDER BY video_id
FOR UPDATE OF youtube_live_sessions
