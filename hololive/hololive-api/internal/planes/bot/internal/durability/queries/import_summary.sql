SELECT 'bot_webhook_inbox' AS relation, inbox.status AS bucket, count(*) AS row_count
FROM bot_webhook_inbox AS inbox
GROUP BY inbox.status
UNION ALL
SELECT 'bot_webhook_heads', '', count(*) FROM bot_webhook_heads
UNION ALL
SELECT 'bot_command_executions', command.status, count(*)
FROM bot_command_executions AS command
GROUP BY command.status
UNION ALL
SELECT 'bot_reply_outbox', outbox.status, count(*)
FROM bot_reply_outbox AS outbox
GROUP BY outbox.status
UNION ALL
SELECT 'bot_reply_outbox_replay_audit', audit.event_type, count(*)
FROM bot_reply_outbox_replay_audit AS audit
GROUP BY audit.event_type
UNION ALL
SELECT 'bot_reply_outbox_resolution_audit', '', count(*) FROM bot_reply_outbox_resolution_audit
UNION ALL
SELECT 'bot_webhook_nonces', '', count(*) FROM bot_webhook_nonces
