SELECT group_key, provider, external_id, video_id, channel_id, title, scheduled_at, ended_at, is_live
FROM youtube_schedule_items
WHERE group_key = $1
FOR UPDATE
