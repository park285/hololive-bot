INSERT INTO youtube_schedule_items (
    group_key, provider, external_id, video_id, channel_id, title, scheduled_at, ended_at, is_live, collabo_talent_names, observed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (group_key, provider, external_id) DO UPDATE SET
    video_id = excluded.video_id,
    channel_id = excluded.channel_id,
    title = excluded.title,
    scheduled_at = excluded.scheduled_at,
    ended_at = excluded.ended_at,
    is_live = excluded.is_live,
    collabo_talent_names = excluded.collabo_talent_names,
    observed_at = excluded.observed_at,
    updated_at = NOW()
WHERE youtube_schedule_items.observed_at IS NULL
   OR excluded.observed_at > youtube_schedule_items.observed_at
