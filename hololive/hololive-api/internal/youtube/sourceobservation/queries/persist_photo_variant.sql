INSERT INTO youtube_channel_photo_variants (
    channel_id, kind, provider, scheduled_for, url, width, height,
    stable_media_id, content_fingerprint, observation_id, effective_at, received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (channel_id, kind, provider, scheduled_for) DO UPDATE SET
    url = excluded.url,
    width = excluded.width,
    height = excluded.height,
    stable_media_id = excluded.stable_media_id,
    content_fingerprint = excluded.content_fingerprint,
    observation_id = excluded.observation_id,
    effective_at = excluded.effective_at,
    received_at = excluded.received_at
WHERE (youtube_channel_photo_variants.url,
       youtube_channel_photo_variants.width,
       youtube_channel_photo_variants.height,
       youtube_channel_photo_variants.stable_media_id,
       youtube_channel_photo_variants.content_fingerprint,
       youtube_channel_photo_variants.observation_id,
       youtube_channel_photo_variants.effective_at,
       youtube_channel_photo_variants.received_at)
IS DISTINCT FROM (excluded.url, excluded.width, excluded.height,
                  excluded.stable_media_id, excluded.content_fingerprint,
                  excluded.observation_id, excluded.effective_at, excluded.received_at)
