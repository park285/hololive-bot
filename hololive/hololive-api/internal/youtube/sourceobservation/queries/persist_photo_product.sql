INSERT INTO youtube_channel_profiles (channel_id, avatar, banner, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id) DO UPDATE SET
    avatar = COALESCE(excluded.avatar, youtube_channel_profiles.avatar),
    banner = COALESCE(excluded.banner, youtube_channel_profiles.banner),
    updated_at = excluded.updated_at
WHERE (youtube_channel_profiles.avatar, youtube_channel_profiles.banner)
IS DISTINCT FROM (
    COALESCE(excluded.avatar, youtube_channel_profiles.avatar),
    COALESCE(excluded.banner, youtube_channel_profiles.banner)
)
