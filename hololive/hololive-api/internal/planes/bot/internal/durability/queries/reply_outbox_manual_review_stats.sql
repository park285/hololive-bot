SELECT count(id), coalesce(?1 - min(updated_at), 0)
FROM bot_reply_outbox
WHERE status = 'manual_review'
