SELECT video_id
FROM youtube_live_reconciliation_heads
WHERE status IN ('UPCOMING', 'LIVE')
ORDER BY video_id
LIMIT $1
