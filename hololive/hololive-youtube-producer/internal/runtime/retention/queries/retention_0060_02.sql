
WITH picked AS (
	SELECT l.video_id
	FROM youtube_live_sessions l
	WHERE l.status = 'ENDED' AND l.ended_at < $1
	  AND NOT EXISTS (
		SELECT 1 FROM youtube_live_viewer_samples s WHERE s.video_id = l.video_id
	)
	ORDER BY l.ended_at ASC, l.video_id ASC
	LIMIT $2
)
DELETE FROM youtube_live_sessions l
USING picked
WHERE l.video_id = picked.video_id