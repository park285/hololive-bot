SELECT channel_id,
       handle_set, handle, handle_effective_at,
       description_set, description, description_effective_at,
       description_empty_slots, description_empty_first_scheduled_for,
       description_empty_last_scheduled_for, description_empty_first_received_at,
       country_set, country, country_effective_at,
       country_empty_slots, country_empty_first_scheduled_for,
       country_empty_last_scheduled_for, country_empty_first_received_at,
       joined_date_set, joined_date, joined_date_effective_at
FROM youtube_channel_profile_heads
WHERE channel_id = $1
FOR UPDATE
