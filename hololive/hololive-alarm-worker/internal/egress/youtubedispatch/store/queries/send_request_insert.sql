INSERT INTO youtube_notification_send_request(base_id, room_id, message, message_hash, dedupe_keys, member_ids, route)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (base_id) DO NOTHING;
