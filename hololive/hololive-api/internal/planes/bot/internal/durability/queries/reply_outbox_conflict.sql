SELECT payload_hash, client_request_id
FROM bot_reply_outbox
WHERE message_id = $1
  AND phase = $2
  AND ordinal = $3
