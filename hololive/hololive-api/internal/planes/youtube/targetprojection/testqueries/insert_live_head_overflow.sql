INSERT INTO youtube_live_reconciliation_heads (video_id, status)
SELECT 'vid-' || lpad(value::text, 5, '0'), 'LIVE'
FROM generate_series(1, $1) AS value
