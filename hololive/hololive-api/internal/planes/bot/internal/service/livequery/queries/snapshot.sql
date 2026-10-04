WITH clock AS MATERIALIZED (
    SELECT statement_timestamp() AS as_of
), roster AS MATERIALIZED (
    SELECT channel_id,
           string_agg(DISTINCT COALESCE(NULLIF(org, ''), 'Hololive'), ' / ' ORDER BY COALESCE(NULLIF(org, ''), 'Hololive')) AS org,
           string_agg(DISTINCT COALESCE(NULLIF(korean_name, ''), NULLIF(english_name, ''), channel_id), ' / '
                      ORDER BY COALESCE(NULLIF(korean_name, ''), NULLIF(english_name, ''), channel_id)) AS channel_name
    FROM members
    WHERE is_graduated = false AND btrim(channel_id) <> ''
      AND (($1 AND (org = 'Hololive' OR org = '')) OR (NOT $1 AND channel_id = $2))
    GROUP BY channel_id
    ORDER BY channel_id
    LIMIT $4
), generation AS MATERIALIZED (
    SELECT generation FROM youtube_collection_projection_generations, clock
    WHERE status = 'CURRENT' AND valid_until > as_of
), targets AS MATERIALIZED (
    SELECT r.channel_id, r.channel_name, r.org, clock.as_of,
           EXISTS (SELECT 1 FROM generation) AS projection_valid,
           t.subject_key IS NOT NULL AS collected,
           c.subject_key IS NOT NULL AS check_collected,
           LEAST(INTERVAL '5 minutes', (2 * t.poll_interval_ms + 30000) * INTERVAL '1 millisecond') AS budget,
           LEAST(INTERVAL '5 minutes', (2 * c.poll_interval_ms + 30000) * INTERVAL '1 millisecond') AS check_budget
    FROM roster r CROSS JOIN clock
    LEFT JOIN youtube_collection_targets t
      ON t.subject_key = r.channel_id AND t.observation_kind = 'live_snapshot'
     AND t.projection_generation IN (SELECT generation FROM generation)
     AND t.enabled
    LEFT JOIN youtube_collection_targets c
      ON c.subject_key = r.channel_id AND c.observation_kind = 'channel_live_check'
     AND c.projection_generation IN (SELECT generation FROM generation)
     AND c.enabled
), coverage AS MATERIALIZED (
    -- 목록 누락과 absence slot은 부재 근거가 아니다. 최신 /live의 검증된 음성만 읽는다.
    SELECT t.channel_id, c.effective_at AS covered_at
    FROM targets t JOIN youtube_channel_live_checks c USING (channel_id)
    WHERE t.check_collected AND c.channel_identity_confirmed
      AND c.outcome IN ('UPCOMING_VIDEO', 'CHANNEL_PAGE')
      AND c.scheduled_for BETWEEN t.as_of - t.check_budget AND t.as_of
      AND c.effective_at BETWEEN t.as_of - t.check_budget AND t.as_of
      AND c.observed_at BETWEEN t.as_of - t.check_budget AND t.as_of
      AND c.received_at BETWEEN t.as_of - t.check_budget AND t.as_of
), unavailable AS MATERIALIZED (
    -- 신선한 positive와 비정상 session/head는 가용성으로 덮지 않는다.
    SELECT s.video_id
    FROM youtube_live_sessions s JOIN targets t USING (channel_id)
    JOIN youtube_live_reconciliation_heads h USING (video_id)
    JOIN youtube_video_availability a ON a.video_id = s.video_id AND a.channel_id = s.channel_id
    JOIN youtube_collection_targets v
      ON v.subject_key = s.video_id AND v.observation_kind = 'video_live_check'
     AND v.projection_generation IN (SELECT generation FROM generation)
     AND v.enabled
    CROSS JOIN LATERAL (
        SELECT LEAST(INTERVAL '5 minutes', (2 * v.poll_interval_ms + 30000) * INTERVAL '1 millisecond') AS budget
    ) validity
    WHERE s.status = 'LIVE' AND h.status = 'LIVE'
      AND a.identity_confirmed AND a.availability = 'PUBLIC_UNAVAILABLE' AND a.method = 'player_private'
      AND a.scheduled_for BETWEEN t.as_of - validity.budget AND t.as_of
      AND a.effective_at BETWEEN t.as_of - validity.budget AND t.as_of
      AND a.observed_at BETWEEN t.as_of - validity.budget AND t.as_of
      AND a.received_at BETWEEN t.as_of - validity.budget AND t.as_of
      AND (h.last_live_positive_at IS NULL OR a.effective_at > h.last_live_positive_at)
      AND (h.last_live_positive_seen_at IS NULL OR h.last_live_positive_seen_at <= t.as_of)
      AND (s.started_at IS NULL OR s.started_at <= t.as_of)
      AND NOT COALESCE(h.last_live_positive_at BETWEEN t.as_of - t.budget AND t.as_of
                       AND h.last_live_positive_seen_at BETWEEN t.as_of - t.budget AND t.as_of, false)
), pending_state AS MATERIALIZED (
    SELECT p.video_id, p.channel_id, p.kind, p.effective_at,
           s.video_id IS NOT NULL AS has_session, h.video_id IS NOT NULL AS has_head,
           s.channel_id AS session_channel_id, s.status AS session_status, h.status AS head_status,
           h.last_live_positive_at, h.last_upcoming_positive_at
    FROM youtube_live_pending_ends p
    LEFT JOIN youtube_live_sessions s USING (video_id)
    LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
    WHERE p.channel_id IN (SELECT channel_id FROM roster) OR s.channel_id IN (SELECT channel_id FROM roster)
), orphan_diagnostics AS MATERIALIZED (
    SELECT channel_id, count(video_id) AS retained_orphan_ends
    FROM pending_state
    WHERE kind = 'EXPLICIT_END' AND NOT has_session AND NOT has_head
    GROUP BY channel_id
), ended_pending_diagnostics AS MATERIALIZED (
    -- D2는 pending 채널과 canonical session 채널 어느 조회에서도 보이게 하고, 같은 채널이면 한 번만 센다.
    SELECT r.channel_id, count(DISTINCT p.video_id) AS ended_pending_ends
    FROM pending_state p
    CROSS JOIN LATERAL (VALUES (p.channel_id), (p.session_channel_id)) attributed(channel_id)
    JOIN roster r ON r.channel_id = attributed.channel_id
    WHERE p.session_status = 'ENDED'
    GROUP BY r.channel_id
), ended_head_diagnostics AS MATERIALIZED (
    SELECT s.channel_id, count(s.video_id) AS ended_head_mismatches
    FROM youtube_live_sessions s JOIN roster r USING (channel_id)
    LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
    WHERE s.status = 'ENDED' AND h.status IS DISTINCT FROM 'ENDED'
    GROUP BY s.channel_id
), unresolved_pending AS MATERIALIZED (
    -- D1/D2는 보존 진단일 뿐 현재 후보가 아니다. head-only와 channel 불일치는 계속 차단한다.
    SELECT p.video_id, p.channel_id, p.has_session OR p.has_head AS has_state
    FROM pending_state p
    WHERE p.session_status IS DISTINCT FROM 'ENDED'
      AND NOT (p.kind = 'EXPLICIT_END' AND NOT p.has_session AND NOT p.has_head)
      AND (p.channel_id IS DISTINCT FROM p.session_channel_id OR NOT COALESCE(
          p.session_status = p.head_status AND p.session_status IN ('LIVE', 'UPCOMING')
          AND (p.effective_at <= p.last_live_positive_at
               OR (p.kind = 'EXPLICIT_CANCEL' AND p.effective_at <= p.last_upcoming_positive_at)), false))
      AND NOT (p.channel_id = p.session_channel_id AND p.video_id IN (SELECT video_id FROM unavailable))
), pending_channels AS MATERIALIZED (
    SELECT DISTINCT channel_id FROM unresolved_pending
), active_ids AS MATERIALIZED (
    SELECT s.video_id FROM youtube_live_sessions s JOIN roster r USING (channel_id) WHERE s.status = 'LIVE'
    UNION
    SELECT h.video_id FROM youtube_live_reconciliation_heads h
    JOIN youtube_live_sessions s USING (video_id) JOIN roster r USING (channel_id)
    WHERE h.status = 'LIVE' AND s.status <> 'ENDED'
    UNION
    SELECT video_id FROM unresolved_pending WHERE has_state
), states AS MATERIALIZED (
    SELECT v.video_id, COALESCE(s.channel_id, p.channel_id) AS channel_id,
           s.title, s.started_at, s.status AS session_status, h.status AS head_status,
           h.last_live_positive_at, h.last_live_positive_seen_at,
           a.video_id IS NOT NULL AND (p.video_id IS NULL OR p.channel_id = s.channel_id) AS public_unavailable,
           p.video_id IS NOT NULL OR h.end_candidate_kind IS NOT NULL AS pending,
           s.status IS DISTINCT FROM h.status
             OR (p.video_id IS NOT NULL AND p.channel_id <> s.channel_id) AS inconsistent
    FROM active_ids v
    LEFT JOIN youtube_live_sessions s USING (video_id)
    LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
    LEFT JOIN unresolved_pending p USING (video_id)
    LEFT JOIN unavailable a USING (video_id)
), facts AS MATERIALIZED (
    SELECT s.video_id, s.channel_id, s.title, s.started_at, s.session_status, s.head_status,
           s.last_live_positive_at, s.last_live_positive_seen_at, s.pending, s.inconsistent, s.public_unavailable,
           t.channel_name, t.org, t.projection_valid, t.collected,
           s.last_live_positive_at > t.as_of OR s.last_live_positive_seen_at > t.as_of OR s.started_at > t.as_of AS future_clock,
           s.last_live_positive_at BETWEEN t.as_of - t.budget AND t.as_of
             AND s.last_live_positive_seen_at BETWEEN t.as_of - t.budget AND t.as_of AS fresh
    FROM states s JOIN targets t USING (channel_id)
), diagnostics AS (
    SELECT t.channel_id, c.covered_at,
           jsonb_build_object('retained_orphan_ends', COALESCE(o.retained_orphan_ends, 0),
                              'ended_pending_ends', COALESCE(e.ended_pending_ends, 0),
                              'ended_head_mismatches', COALESCE(m.ended_head_mismatches, 0)) AS diagnostics,
           CASE WHEN NOT t.projection_valid THEN 'invalid_projection'
                WHEN NOT t.collected OR NOT t.check_collected THEN 'uncollected'
                WHEN COALESCE(bool_or(f.inconsistent), false) THEN 'inconsistent'
                WHEN p.channel_id IS NOT NULL OR COALESCE(bool_or(f.pending AND NOT f.public_unavailable), false) THEN 'confirming_end'
                WHEN COALESCE(bool_or(f.future_clock), false) THEN 'invalid_clock'
                WHEN COALESCE(bool_or(f.session_status = 'LIVE' AND NOT COALESCE(f.fresh, false) AND NOT f.public_unavailable), false) THEN 'stale'
                WHEN c.covered_at IS NULL THEN 'incomplete'
                ELSE 'covered' END AS reason
    FROM targets t
    LEFT JOIN coverage c USING (channel_id) LEFT JOIN facts f USING (channel_id)
    LEFT JOIN pending_channels p USING (channel_id)
    LEFT JOIN orphan_diagnostics o USING (channel_id)
    LEFT JOIN ended_pending_diagnostics e USING (channel_id) LEFT JOIN ended_head_diagnostics m USING (channel_id)
    GROUP BY t.channel_id, t.projection_valid, t.collected, t.check_collected, c.covered_at, p.channel_id,
             o.retained_orphan_ends, e.ended_pending_ends, m.ended_head_mismatches
), items AS (
    SELECT video_id, channel_id, channel_name, org, title, started_at, last_live_positive_at AS observed_at
    FROM facts
    WHERE projection_valid AND collected AND session_status = 'LIVE' AND head_status = 'LIVE'
      AND fresh AND NOT COALESCE(future_clock, false) AND NOT pending AND NOT inconsistent
    ORDER BY started_at DESC NULLS LAST, channel_name, video_id
    LIMIT $3
)
SELECT clock.as_of,
       COALESCE((SELECT jsonb_agg(d ORDER BY d.channel_id) FROM diagnostics d), '[]'::jsonb),
       COALESCE((SELECT jsonb_agg(i ORDER BY i.started_at DESC NULLS LAST, i.channel_name, i.video_id) FROM items i), '[]'::jsonb)
FROM clock;
