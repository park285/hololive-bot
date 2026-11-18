SELECT pg_advisory_xact_lock(hashtextextended(ordering_key, 0))
FROM bot_webhook_inbox
WHERE message_id = $1
