SELECT request.base_id, request.room_id, request.message, request.message_hash, request.route,
       request.dedupe_keys, request.member_ids, request.generation
FROM youtube_notification_send_request request
WHERE request.base_id IN (
 SELECT send_request_id FROM youtube_notification_delivery WHERE id = ANY($1::bigint[])
);
