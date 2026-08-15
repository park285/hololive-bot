SELECT video_id
FROM youtube_live_reconciliation_heads
WHERE next_end_check_at IS NOT NULL
  AND next_end_check_at <= NOW()
ORDER BY next_end_check_at, video_id
LIMIT 1
FOR UPDATE SKIP LOCKED
