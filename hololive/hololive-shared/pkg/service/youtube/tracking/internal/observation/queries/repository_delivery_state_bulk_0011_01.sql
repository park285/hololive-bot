
WITH input AS (
	SELECT *
	FROM unnest(
		$1::text[],
		$2::text[],
		$3::text[],
		$4::text[],
		$5::timestamptz[],
		$6::timestamptz[]
	) AS t(kind, content_id, canonical_content_id, raw_content_id, alarm_sent_at, authorized_at)
), deduped_input AS (
	SELECT DISTINCT ON (kind, canonical_content_id)
		kind,
		content_id,
		canonical_content_id,
		raw_content_id,
		alarm_sent_at,
		authorized_at
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
	        WHEN CAST(ROUND(EXTRACT(EPOCH FROM (i.alarm_sent_at - t.actual_published_at)) * 1000) AS BIGINT) > 120000 THEN TRUE
	        ELSE FALSE
	    END,
	    delivery_status = 'SENT',
	    updated_at = $7
	FROM deduped_input AS i
	WHERE t.kind = i.kind
	  AND (
		t.canonical_content_id = i.canonical_content_id
		OR t.content_id = i.content_id
		OR t.content_id = i.raw_content_id
	  )
	  AND (t.alarm_sent_at IS NULL OR t.alarm_sent_at > i.alarm_sent_at)
	RETURNING
		t.kind,
		CASE
			WHEN t.canonical_content_id <> '' THEN t.canonical_content_id
			ELSE i.canonical_content_id
		END AS post_id,
		t.content_id,
		t.channel_id,
		t.actual_published_at,
		t.detected_at,
		t.alarm_sent_at
), claimed_state_finalized AS (
	UPDATE youtube_community_shorts_alarm_states AS s
	SET authorized_at = NULL,
	    alarm_sent_at = i.alarm_sent_at,
	    delivery_status = 'SENT',
	    updated_at = $7
	FROM deduped_input AS i
	WHERE s.kind = i.kind
	  AND (
		s.post_id = i.canonical_content_id
		OR s.content_id = i.content_id
		OR s.content_id = i.raw_content_id
	  )
	  AND i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND i.authorized_at IS NOT NULL
	  AND s.authorized_at = i.authorized_at
	  AND s.alarm_sent_at IS NULL
	RETURNING s.kind, s.post_id
), authorization_mismatches AS (
	SELECT s.kind, s.post_id
	FROM deduped_input AS i
	JOIN youtube_community_shorts_alarm_states AS s
	  ON s.kind = i.kind
	 AND (
		s.post_id = i.canonical_content_id
		OR s.content_id = i.content_id
		OR s.content_id = i.raw_content_id
	 )
	WHERE i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND i.authorized_at IS NOT NULL
	  AND s.alarm_sent_at IS NULL
	  AND NOT EXISTS (
		SELECT 1
		FROM claimed_state_finalized AS f
		WHERE f.kind = s.kind
		  AND f.post_id = s.post_id
	  )
	), alarm_state_insert_candidates AS (
		SELECT DISTINCT ON (t.kind, post_id)
			t.kind,
		CASE
			WHEN t.canonical_content_id <> '' THEN t.canonical_content_id
			ELSE i.canonical_content_id
		END AS post_id,
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
	 AND (
		t.canonical_content_id = i.canonical_content_id
		OR t.content_id = i.content_id
		OR t.content_id = i.raw_content_id
	 )
		WHERE i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
		ORDER BY t.kind, post_id, alarm_sent_at ASC
	), legacy_state_repointed AS (
		UPDATE youtube_community_shorts_alarm_states AS s
		SET post_id = t.post_id,
		    actual_published_at = COALESCE(s.actual_published_at, t.actual_published_at),
		    detected_at = CASE
		        WHEN t.detected_at < s.detected_at THEN t.detected_at
		        ELSE s.detected_at
		    END,
		    authorized_at = NULL,
		    alarm_sent_at = CASE
		        WHEN s.alarm_sent_at IS NULL OR s.alarm_sent_at > t.alarm_sent_at THEN t.alarm_sent_at
		        ELSE s.alarm_sent_at
		    END,
		    delivery_status = 'SENT',
		    updated_at = $7
		FROM alarm_state_insert_candidates AS t
		WHERE s.kind = t.kind
		  AND s.content_id = t.content_id
		  AND s.post_id <> t.post_id
		  AND NOT EXISTS (
			SELECT 1
			FROM claimed_state_finalized AS f
			WHERE f.kind = s.kind
			  AND f.post_id = s.post_id
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM authorization_mismatches AS m
			WHERE m.kind = s.kind
			  AND m.post_id = s.post_id
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM youtube_community_shorts_alarm_states AS existing
			WHERE existing.kind = t.kind
			  AND existing.post_id = t.post_id
		  )
		RETURNING s.kind, s.content_id
	), existing_state_updated AS (
		UPDATE youtube_community_shorts_alarm_states AS s
		SET authorized_at = NULL,
		    alarm_sent_at = CASE
		        WHEN s.alarm_sent_at IS NULL OR s.alarm_sent_at > i.alarm_sent_at THEN i.alarm_sent_at
		        ELSE s.alarm_sent_at
		    END,
		    delivery_status = 'SENT',
		    updated_at = $7
		FROM deduped_input AS i
		WHERE s.kind = i.kind
		  AND (
			s.post_id = i.canonical_content_id
			OR s.content_id = i.content_id
			OR s.content_id = i.raw_content_id
		  )
		  AND i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
		  AND NOT EXISTS (
			SELECT 1
			FROM claimed_state_finalized AS f
			WHERE f.kind = s.kind
			  AND f.post_id = s.post_id
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM authorization_mismatches AS m
			WHERE m.kind = s.kind
			  AND m.post_id = s.post_id
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM legacy_state_repointed AS r
			WHERE r.kind = s.kind
			  AND r.content_id = s.content_id
		  )
		  AND (s.alarm_sent_at IS NULL OR s.alarm_sent_at > i.alarm_sent_at OR s.authorized_at IS NOT NULL)
		RETURNING s.kind, s.post_id
	), missing_state_inserted AS (
		INSERT INTO youtube_community_shorts_alarm_states (
		kind,
		post_id,
		content_id,
		channel_id,
		actual_published_at,
		detected_at,
		authorized_at,
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
		NULL,
		t.alarm_sent_at,
		'SENT',
		$7,
		$7
	FROM alarm_state_insert_candidates AS t
		WHERE NOT EXISTS (
			SELECT 1
			FROM youtube_community_shorts_alarm_states AS s
			WHERE s.kind = t.kind
			  AND (
				s.post_id = t.post_id
				OR s.content_id = t.content_id
			  )
		)
	ON CONFLICT (kind, post_id) DO UPDATE
	SET alarm_sent_at = CASE
	        WHEN youtube_community_shorts_alarm_states.alarm_sent_at IS NULL
	          OR youtube_community_shorts_alarm_states.alarm_sent_at > EXCLUDED.alarm_sent_at
	        THEN EXCLUDED.alarm_sent_at
	        ELSE youtube_community_shorts_alarm_states.alarm_sent_at
	    END,
	    delivery_status = 'SENT',
	    authorized_at = NULL,
	    updated_at = $7
	RETURNING kind, post_id
)
SELECT
	(SELECT COUNT(*) FROM tracking_updated) AS tracking_updated_count,
	(SELECT COUNT(*) FROM claimed_state_finalized) AS claimed_state_finalized_count,
	(SELECT COUNT(*) FROM authorization_mismatches) AS authorization_mismatch_count,
	(SELECT COUNT(*) FROM existing_state_updated) AS existing_state_updated_count,
	(SELECT COUNT(*) FROM missing_state_inserted) AS missing_state_inserted_count
