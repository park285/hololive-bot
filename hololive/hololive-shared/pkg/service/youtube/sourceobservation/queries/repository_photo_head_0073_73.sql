SELECT kind, identity, url, width, height, effective_at,
       candidate_identity, candidate_url, candidate_width, candidate_height,
       candidate_slots, candidate_first_scheduled_for,
       candidate_last_scheduled_for, candidate_first_received_at
FROM youtube_channel_photo_heads
WHERE channel_id = $1
FOR UPDATE
