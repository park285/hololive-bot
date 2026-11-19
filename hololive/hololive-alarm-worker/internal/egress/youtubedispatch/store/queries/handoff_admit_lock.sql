SELECT r.base_id
FROM youtube_notification_send_request r
WHERE r.base_id = $1
  AND r.generation = $2::INT
  AND r.member_ids @> $3::BIGINT[]
  AND r.member_ids <@ $3::BIGINT[]
FOR UPDATE
