SELECT video_id, ignored_absence_scheduled_for
FROM youtube_live_reconciliation_heads
WHERE video_id = ANY($1::text[])
ORDER BY video_id
