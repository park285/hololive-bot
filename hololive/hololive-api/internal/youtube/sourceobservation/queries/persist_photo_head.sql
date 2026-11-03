INSERT INTO youtube_channel_photo_heads (
    channel_id, kind, identity, url, width, height, effective_at,
    candidate_identity, candidate_url, candidate_width, candidate_height,
    candidate_slots, candidate_first_scheduled_for,
    candidate_last_scheduled_for, candidate_first_received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (channel_id, kind) DO UPDATE SET
    identity = excluded.identity,
    url = excluded.url,
    width = excluded.width,
    height = excluded.height,
    effective_at = excluded.effective_at,
    candidate_identity = excluded.candidate_identity,
    candidate_url = excluded.candidate_url,
    candidate_width = excluded.candidate_width,
    candidate_height = excluded.candidate_height,
    candidate_slots = excluded.candidate_slots,
    candidate_first_scheduled_for = excluded.candidate_first_scheduled_for,
    candidate_last_scheduled_for = excluded.candidate_last_scheduled_for,
    candidate_first_received_at = excluded.candidate_first_received_at,
    updated_at = NOW()
WHERE (youtube_channel_photo_heads.identity, youtube_channel_photo_heads.url,
       youtube_channel_photo_heads.width, youtube_channel_photo_heads.height,
       youtube_channel_photo_heads.effective_at,
       youtube_channel_photo_heads.candidate_identity, youtube_channel_photo_heads.candidate_url,
       youtube_channel_photo_heads.candidate_width, youtube_channel_photo_heads.candidate_height,
       youtube_channel_photo_heads.candidate_slots,
       youtube_channel_photo_heads.candidate_first_scheduled_for,
       youtube_channel_photo_heads.candidate_last_scheduled_for,
       youtube_channel_photo_heads.candidate_first_received_at)
IS DISTINCT FROM (excluded.identity, excluded.url, excluded.width, excluded.height,
                  excluded.effective_at, excluded.candidate_identity, excluded.candidate_url,
                  excluded.candidate_width, excluded.candidate_height, excluded.candidate_slots,
                  excluded.candidate_first_scheduled_for, excluded.candidate_last_scheduled_for,
                  excluded.candidate_first_received_at)
