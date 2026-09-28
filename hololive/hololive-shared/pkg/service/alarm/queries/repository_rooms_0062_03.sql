INSERT INTO alarm_room_display_names (room_id, display_name, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (room_id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    updated_at = EXCLUDED.updated_at
WHERE alarm_room_display_names.display_name IS DISTINCT FROM EXCLUDED.display_name
