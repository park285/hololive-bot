
WITH input AS (
	SELECT kind, canonical_content_id, alarm_sent_at
	FROM unnest(
		$1::text[],
		$2::text[],
		$3::timestamptz[]
	) AS t(kind, canonical_content_id, alarm_sent_at)
), deduped_input AS (
	SELECT DISTINCT ON (kind, canonical_content_id)
		kind,
		canonical_content_id,
		alarm_sent_at
	FROM input
	WHERE kind <> ''
	  AND canonical_content_id <> ''
	  AND alarm_sent_at IS NOT NULL
	ORDER BY kind, canonical_content_id, alarm_sent_at ASC
), tracking_updated AS (
	UPDATE youtube_content_alarm_tracking AS t
	SET alarm_sent_at = i.alarm_sent_at,
	    alarm_latency_millis = CASE
	        WHEN t.actual_published_at IS NULL THEN NULL
	        ELSE CAST(ROUND(EXTRACT(EPOCH FROM (i.alarm_sent_at - t.actual_published_at)) * 1000) AS BIGINT)
	    END,
	    alarm_latency_exceeded = CASE
	        WHEN t.actual_published_at IS NULL THEN NULL
	        WHEN CAST(ROUND(EXTRACT(EPOCH FROM (i.alarm_sent_at - t.actual_published_at)) * 1000) AS BIGINT) > $5::bigint THEN TRUE
	        ELSE FALSE
	    END,
	    delivery_status = 'SENT',
	    updated_at = $4
	FROM deduped_input AS i
	WHERE t.kind = i.kind
	  AND t.canonical_content_id = i.canonical_content_id
	  AND (t.alarm_sent_at IS NULL OR t.alarm_sent_at > i.alarm_sent_at)
	RETURNING
		t.kind,
		t.canonical_content_id AS post_id,
		t.content_id,
		t.channel_id,
		t.actual_published_at,
		t.detected_at,
		t.alarm_sent_at
), alarm_state_insert_candidates AS (
	SELECT DISTINCT ON (t.kind, post_id)
		t.kind,
		t.canonical_content_id AS post_id,
		t.content_id,
		t.channel_id,
		t.actual_published_at,
		t.detected_at,
		CASE
			WHEN t.alarm_sent_at IS NULL OR t.alarm_sent_at > i.alarm_sent_at THEN i.alarm_sent_at
			ELSE t.alarm_sent_at
		END AS alarm_sent_at
	FROM deduped_input AS i
	JOIN youtube_content_alarm_tracking AS t
	  ON t.kind = i.kind
	 AND t.canonical_content_id = i.canonical_content_id
	WHERE i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	ORDER BY t.kind, post_id, alarm_sent_at ASC
), existing_state_updated AS (
	-- 미발송 행은 delivery_status와 무관하게 SENT로 수렴한다.
	UPDATE youtube_community_shorts_alarm_states AS s
	SET alarm_sent_at = CASE
	        WHEN s.alarm_sent_at IS NULL OR s.alarm_sent_at > i.alarm_sent_at THEN i.alarm_sent_at
	        ELSE s.alarm_sent_at
	    END,
	    delivery_status = 'SENT',
	    updated_at = $4
	FROM deduped_input AS i
	WHERE s.kind = i.kind
	  AND s.post_id = i.canonical_content_id
	  AND i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND (s.alarm_sent_at IS NULL OR s.alarm_sent_at > i.alarm_sent_at)
	RETURNING s.kind, s.post_id
), missing_state_inserted AS (
	INSERT INTO youtube_community_shorts_alarm_states (
		kind,
		post_id,
		content_id,
		channel_id,
		actual_published_at,
		detected_at,
		alarm_sent_at,
		delivery_status,
		created_at,
		updated_at
	)
	SELECT
		t.kind,
		t.post_id,
		t.content_id,
		t.channel_id,
		t.actual_published_at,
		t.detected_at,
		t.alarm_sent_at,
		'SENT',
		$4,
		$4
	FROM alarm_state_insert_candidates AS t
	WHERE NOT EXISTS (
		SELECT 1
		FROM youtube_community_shorts_alarm_states AS s
		WHERE s.kind = t.kind
		  AND s.post_id = t.post_id
	)
	ON CONFLICT (kind, post_id) DO UPDATE
	SET alarm_sent_at = CASE
	        WHEN youtube_community_shorts_alarm_states.alarm_sent_at IS NULL
	          OR youtube_community_shorts_alarm_states.alarm_sent_at > EXCLUDED.alarm_sent_at
	        THEN EXCLUDED.alarm_sent_at
	        ELSE youtube_community_shorts_alarm_states.alarm_sent_at
	    END,
	    delivery_status = 'SENT',
	    updated_at = $4
	RETURNING kind, post_id
)
SELECT
	(SELECT COUNT(post_id) FROM tracking_updated) AS tracking_updated_count,
	(SELECT COUNT(post_id) FROM existing_state_updated) AS existing_state_updated_count,
	(SELECT COUNT(post_id) FROM missing_state_inserted) AS missing_state_inserted_count
