SELECT ordering_key
FROM bot_webhook_inbox
WHERE message_id = $1
