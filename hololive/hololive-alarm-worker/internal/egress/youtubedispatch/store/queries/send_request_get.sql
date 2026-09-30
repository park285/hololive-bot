SELECT base_id, room_id, message, message_hash, route, dedupe_keys, member_ids, generation
FROM youtube_notification_send_request WHERE base_id = $1;
