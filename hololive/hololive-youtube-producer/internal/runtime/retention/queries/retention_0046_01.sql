
WITH picked AS (
	SELECT channel_id, captured_at
	FROM youtube_channel_stats_snapshots
	WHERE captured_at < $1
	LIMIT $2
)
DELETE FROM youtube_channel_stats_snapshots s
USING picked
WHERE s.channel_id = picked.channel_id AND s.captured_at = picked.captured_at