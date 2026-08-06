WITH candidate_sessions AS MATERIALIZED (
	SELECT l.ended_at, l.video_id
	FROM youtube_live_sessions l
	WHERE l.status = 'ENDED'
		AND l.ended_at < $1
		AND (l.ended_at, l.video_id) > ($2::timestamptz, $3::varchar(20))
	ORDER BY l.ended_at ASC, l.video_id ASC
	LIMIT $4::integer
),
target_session AS MATERIALIZED (
	SELECT candidate.ended_at, candidate.video_id
	FROM candidate_sessions candidate
	JOIN youtube_live_sessions locked_session
		ON locked_session.video_id = candidate.video_id
	JOIN LATERAL (
		SELECT existing_sample.captured_at
		FROM youtube_live_viewer_samples existing_sample
		WHERE existing_sample.video_id = candidate.video_id
		ORDER BY existing_sample.captured_at ASC
		LIMIT 1
	) sample_probe ON TRUE
	WHERE locked_session.status = 'ENDED'
		AND locked_session.ended_at < $1
	ORDER BY candidate.ended_at ASC, candidate.video_id ASC
	LIMIT 1
	FOR UPDATE OF locked_session SKIP LOCKED
),
picked AS MATERIALIZED (
	SELECT sample.video_id, sample.captured_at
	FROM target_session target
	JOIN youtube_live_viewer_samples sample
		ON sample.video_id = target.video_id
	ORDER BY sample.captured_at ASC
	LIMIT $5::integer
),
deleted AS (
	DELETE FROM youtube_live_viewer_samples sample
	USING picked
	WHERE sample.video_id = picked.video_id
		AND sample.captured_at = picked.captured_at
	RETURNING 1
),
page_end AS MATERIALIZED (
	SELECT candidate.ended_at, candidate.video_id
	FROM candidate_sessions candidate
	ORDER BY candidate.ended_at DESC, candidate.video_id DESC
	LIMIT 1
)
SELECT
	(SELECT COUNT(*) FROM deleted) AS deleted_count,
	target.ended_at AS target_ended_at,
	target.video_id AS target_video_id,
	(SELECT COUNT(*) FROM candidate_sessions) AS candidate_count,
	page_end.ended_at AS page_end_ended_at,
	page_end.video_id AS page_end_video_id
FROM (SELECT 1) singleton
LEFT JOIN target_session target ON TRUE
LEFT JOIN page_end ON TRUE
