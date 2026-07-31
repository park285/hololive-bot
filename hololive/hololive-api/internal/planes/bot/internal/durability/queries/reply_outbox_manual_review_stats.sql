SELECT count(id)::bigint,
       COALESCE(EXTRACT(EPOCH FROM (clock_timestamp() - min(updated_at))), 0)::double precision
FROM bot_reply_outbox
WHERE status = 'manual_review'
