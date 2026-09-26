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
           LEAST(INTERVAL '5 minutes', (2 * t.poll_interval_ms + 30000) * INTERVAL '1 millisecond') AS budget
    FROM roster r CROSS JOIN clock
    LEFT JOIN youtube_collection_targets t
      ON t.subject_key = r.channel_id AND t.observation_kind = 'live_snapshot'
     AND t.projection_generation IN (SELECT generation FROM generation)
     AND t.enabled AND t.valid_until > as_of
), recent_coverage AS MATERIALIZED (
    -- 부분 인덱스와 같은 predicate로 최근 eligible LIVE slot을 한 번만 읽는다.
    SELECT a.coverage, a.effective_at, a.received_at, a.scheduled_for
    FROM youtube_live_absence_slots a, clock
    WHERE (a.coverage -> 'filters' -> 'statuses') ? 'LIVE'
      AND a.effective_at BETWEEN as_of - INTERVAL '5 minutes' AND as_of
      AND a.received_at BETWEEN as_of - INTERVAL '5 minutes' AND as_of
      AND a.scheduled_for BETWEEN as_of - INTERVAL '5 minutes' AND as_of
), coverage_channels AS MATERIALIZED (
    -- planner가 target을 먼저 결합해 같은 JSON을 채널 수만큼 펼치지 않게 한다.
    SELECT ids.channel_id, a.effective_at, a.received_at, a.scheduled_for
    FROM recent_coverage a
    CROSS JOIN LATERAL jsonb_array_elements_text(a.coverage -> 'requested_channel_ids') ids(channel_id)
), coverage AS MATERIALIZED (
    SELECT t.channel_id, max(a.effective_at) AS covered_at
    FROM coverage_channels a JOIN targets t USING (channel_id)
    WHERE a.effective_at >= t.as_of - t.budget
      AND a.received_at >= t.as_of - t.budget
      AND a.scheduled_for >= t.as_of - t.budget
    GROUP BY t.channel_id
), unresolved_pending AS MATERIALIZED (
    -- pending은 보존된 증거다. terminal 상태나 positive가 이긴 증거는 현재 종료가 아니다.
    -- NULL 상태 및 channel 불일치는 증거를 버리는 근거로 사용하지 않는다.
    SELECT p.video_id, p.channel_id, s.video_id IS NOT NULL OR h.video_id IS NOT NULL AS has_state
    FROM youtube_live_pending_ends p
    LEFT JOIN youtube_live_sessions s USING (video_id)
    LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
    WHERE (p.channel_id IN (SELECT channel_id FROM roster) OR s.channel_id IN (SELECT channel_id FROM roster))
      AND (p.channel_id IS DISTINCT FROM s.channel_id OR NOT COALESCE(
          (s.status = 'ENDED' AND h.status = 'ENDED')
          OR (s.status = h.status AND s.status IN ('LIVE', 'UPCOMING')
              AND (p.effective_at <= h.last_live_positive_at
                   OR (p.kind = 'EXPLICIT_CANCEL' AND p.effective_at <= h.last_upcoming_positive_at))), false))
), pending_channels AS MATERIALIZED (
    SELECT DISTINCT channel_id FROM unresolved_pending
), active_ids AS MATERIALIZED (
    SELECT s.video_id FROM youtube_live_sessions s JOIN roster r USING (channel_id) WHERE s.status = 'LIVE'
    UNION
    SELECT h.video_id FROM youtube_live_reconciliation_heads h
    JOIN youtube_live_sessions s USING (video_id) JOIN roster r USING (channel_id)
    WHERE h.status = 'LIVE'
    UNION
    -- canonical state 없는 종료는 채널 진단으로 충분하다. 영상별 state join을 반복하지 않는다.
    SELECT video_id FROM unresolved_pending WHERE has_state
), states AS MATERIALIZED (
    SELECT v.video_id, COALESCE(s.channel_id, p.channel_id) AS channel_id,
           s.title, s.started_at, s.status AS session_status, h.status AS head_status,
           h.last_live_positive_at, h.last_live_positive_seen_at,
           p.video_id IS NOT NULL OR h.end_candidate_kind IS NOT NULL AS pending,
           s.status IS DISTINCT FROM h.status
             OR (p.video_id IS NOT NULL AND p.channel_id <> s.channel_id) AS inconsistent
    FROM active_ids v
    LEFT JOIN youtube_live_sessions s USING (video_id)
    LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
    LEFT JOIN unresolved_pending p USING (video_id)
), facts AS MATERIALIZED (
    SELECT s.video_id, s.channel_id, s.title, s.started_at, s.session_status, s.head_status,
           s.last_live_positive_at, s.last_live_positive_seen_at, s.pending, s.inconsistent,
           t.channel_name, t.org, t.projection_valid, t.collected,
           s.last_live_positive_at > t.as_of OR s.last_live_positive_seen_at > t.as_of OR s.started_at > t.as_of AS future_clock,
           s.last_live_positive_at BETWEEN t.as_of - t.budget AND t.as_of
             AND s.last_live_positive_seen_at BETWEEN t.as_of - t.budget AND t.as_of AS fresh
    FROM states s JOIN targets t USING (channel_id)
), diagnostics AS (
    SELECT t.channel_id, c.covered_at,
           CASE WHEN NOT t.projection_valid THEN 'invalid_projection'
                WHEN NOT t.collected THEN 'uncollected'
                WHEN COALESCE(bool_or(f.inconsistent), false) THEN 'inconsistent'
                WHEN p.channel_id IS NOT NULL OR COALESCE(bool_or(f.pending), false) THEN 'confirming_end'
                WHEN COALESCE(bool_or(f.future_clock), false) THEN 'invalid_clock'
                WHEN COALESCE(bool_or(f.session_status = 'LIVE' AND NOT COALESCE(f.fresh, false)), false) THEN 'stale'
                WHEN c.covered_at IS NULL THEN 'incomplete'
                ELSE 'covered' END AS reason
    FROM targets t
    LEFT JOIN coverage c USING (channel_id) LEFT JOIN facts f USING (channel_id)
    LEFT JOIN pending_channels p USING (channel_id)
    GROUP BY t.channel_id, t.projection_valid, t.collected, c.covered_at, p.channel_id
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
