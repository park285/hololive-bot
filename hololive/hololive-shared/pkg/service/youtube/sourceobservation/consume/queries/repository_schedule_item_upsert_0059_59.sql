INSERT INTO youtube_schedule_items (
    group_key, provider, external_id, video_id, channel_id, title, scheduled_at, ended_at, is_live, collabo_talent_names
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (group_key, provider, external_id) DO UPDATE SET
    video_id = excluded.video_id,
    channel_id = excluded.channel_id,
    title = excluded.title,
    scheduled_at = excluded.scheduled_at,
    ended_at = excluded.ended_at,
    is_live = excluded.is_live,
    collabo_talent_names = excluded.collabo_talent_names,
    updated_at = NOW()
WHERE youtube_schedule_items.video_id IS DISTINCT FROM excluded.video_id
   OR youtube_schedule_items.channel_id IS DISTINCT FROM excluded.channel_id
   OR youtube_schedule_items.title IS DISTINCT FROM excluded.title
   OR youtube_schedule_items.scheduled_at IS DISTINCT FROM excluded.scheduled_at
   OR youtube_schedule_items.ended_at IS DISTINCT FROM excluded.ended_at
   OR youtube_schedule_items.is_live IS DISTINCT FROM excluded.is_live
   OR youtube_schedule_items.collabo_talent_names IS DISTINCT FROM excluded.collabo_talent_names
