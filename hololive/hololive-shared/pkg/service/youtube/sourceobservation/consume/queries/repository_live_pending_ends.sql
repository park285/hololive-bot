SELECT video_id, channel_id, kind, observation_id, effective_at, received_at,
       scheduled_for, ended_at, negative_eligible, scope_covers
FROM youtube_live_pending_ends
WHERE video_id = ANY($1::text[])
ORDER BY video_id
FOR UPDATE
