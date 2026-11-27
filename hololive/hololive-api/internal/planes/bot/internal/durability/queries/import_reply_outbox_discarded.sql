UPDATE bot_reply_outbox
SET status = 'discarded', payload = NULL, row_version = row_version + 1
WHERE status = 'manual_review'
  AND id IN (SELECT audit.outbox_id FROM bot_reply_outbox_resolution_audit AS audit)
