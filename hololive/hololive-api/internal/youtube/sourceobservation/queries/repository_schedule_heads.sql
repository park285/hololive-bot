SELECT video_id, status
FROM youtube_live_reconciliation_heads
WHERE video_id = ANY($1::text[])
ORDER BY video_id
FOR UPDATE
