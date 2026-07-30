
        ON CONFLICT (kind, post_id) DO UPDATE
        SET content_id = EXCLUDED.content_id,
            channel_id = EXCLUDED.channel_id,
            actual_published_at = COALESCE(youtube_community_shorts_alarm_states.actual_published_at, EXCLUDED.actual_published_at),
            detected_at = CASE
                WHEN EXCLUDED.detected_at < youtube_community_shorts_alarm_states.detected_at THEN EXCLUDED.detected_at
                ELSE youtube_community_shorts_alarm_states.detected_at
            END,
            authorized_at = 