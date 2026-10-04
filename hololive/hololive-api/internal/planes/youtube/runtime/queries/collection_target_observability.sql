-- rpc_per_kind: 활성 kind마다 helper RPC를 따로 보내는 job(content 목록 2종)이다.
-- 나머지 job은 bundle 한 번에 RPC 1회를 보낸다. 방송 탭 snapshot과 채널 /live 확인은 별도 job·슬롯이다.
-- 수요는 기본 cadence의 명목값이며 재시도와 job 안의 추가 보강 호출은 포함하지 않는다.
-- $1은 기존 LIVE 신선도 예산(ms)이다. not_before가 미래인 bundle은 신선한 근거로 잠든 대상이라
-- due·stale·미완료·완료 나이에서 깨어날 때까지 빠지고, 수요는 예산당 최대 1회로 센다.
-- 미획득 target의 due 기준은 연속 membership 동안 보존되는 created_at이다.
WITH mapping(kind, observation_kinds, rpc_per_kind) AS (
    VALUES
        ('youtubejs_channel_live', ARRAY['live_snapshot'], FALSE),
        ('youtubejs_channel_live_check', ARRAY['channel_live_check'], FALSE),
        ('youtubejs_video_live', ARRAY['video_live_check'], FALSE),
        ('community_collect', ARRAY['community_page'], FALSE),
        ('youtubejs_content', ARRAY['video_list', 'shorts_list'], TRUE),
        ('youtubejs_channel_metadata', ARRAY['channel_profile', 'channel_photo'], FALSE)
), current_projection AS (
    SELECT generation FROM youtube_collection_projection_generations
    WHERE status = 'CURRENT' AND valid_until > statement_timestamp()
), targets AS (
    SELECT m.kind, CASE WHEN m.rpc_per_kind THEN COUNT(t.observation_kind) ELSE 1 END AS rpc_calls, t.subject_key,
           MIN(t.poll_interval_ms) AS interval_ms, MIN(t.created_at) AS created_at,
           -- bundle 안 한 row라도 확인 가능하면 신규 admission 대상이다.
           MIN(COALESCE(t.not_before, '-infinity'::timestamptz)) AS eligible_at
    FROM mapping m
    JOIN youtube_collection_targets t ON t.observation_kind = ANY(m.observation_kinds)
    JOIN current_projection g ON g.generation = t.projection_generation
    WHERE t.enabled AND t.valid_until > statement_timestamp()
    GROUP BY m.kind, m.rpc_per_kind, t.subject_key
), leased AS (
    SELECT t.kind, t.rpc_calls, t.subject_key, t.interval_ms, t.eligible_at, l.last_completed_at,
           CASE
               WHEN l.job_key IS NULL THEN t.created_at
               WHEN l.slot_state = 'IDLE' THEN l.next_due_at
               WHEN l.slot_state = 'DEFERRED' THEN l.retry_not_before
               WHEN l.slot_state = 'ACTIVE' THEN l.lease_expires_at
           END AS lease_due_at
    FROM targets t
    LEFT JOIN youtube_collection_job_leases l
      ON l.job_key = 'collector:youtubejs:' || t.kind || ':' || t.subject_key
), samples AS (
    SELECT kind, rpc_calls, subject_key, interval_ms, eligible_at, last_completed_at,
           GREATEST(lease_due_at, eligible_at) AS due_at,
           eligible_at > statement_timestamp() AS sleeping
    FROM leased
), active_live_videos AS (
    SELECT video_id FROM youtube_live_reconciliation_heads WHERE status IN ('LIVE', 'UPCOMING')
    UNION
    SELECT video_id FROM youtube_live_sessions WHERE status IN ('LIVE', 'UPCOMING')
), live_states AS MATERIALIZED (
    SELECT v.video_id, h.status AS state, product.status AS product_state, product.scheduled_start_time, product.lifecycle_origin,
           availability.observed_at AS checked_at
    FROM active_live_videos v
    JOIN youtube_live_sessions product ON product.video_id = v.video_id
    JOIN targets t ON t.kind = 'youtubejs_channel_live' AND t.subject_key = product.channel_id
    LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id = v.video_id
    LEFT JOIN youtube_video_availability availability ON availability.video_id=v.video_id
), live_review_videos AS MATERIALIZED (
    -- 영수증이 있는 현재 영상만 고정해 빈 영수증 집합에서 snapshot 계산을 건너뜁니다.
    SELECT receipt.video_id FROM (SELECT DISTINCT video_id FROM youtube_live_review_receipts) receipt
    WHERE EXISTS (SELECT 1 FROM live_states state WHERE state.video_id=receipt.video_id)
), live_reviews AS MATERIALIZED (
    -- 영상별 현재 snapshot 비교를 한 번만 계산하며 과거 영수증으로 미상을 닫지 않습니다.
    SELECT video.video_id,
           (SELECT max(receipt.recorded_at) FROM youtube_live_review_receipts receipt
               WHERE receipt.video_id=video.video_id AND receipt.snapshot_sha256=
                   (SELECT snapshot_sha256 FROM youtube_live_review_snapshot(video.video_id))) AS reviewed_at
    FROM live_review_videos video
), reviewed_live_states AS (
    -- recorded_at은 NOT NULL이므로 현재 snapshot의 영수증 존재와 같은 판정입니다.
    SELECT state.video_id, state.state, state.product_state, state.scheduled_start_time,
           state.lifecycle_origin, state.checked_at, review.reviewed_at,
           review.reviewed_at IS NOT NULL AS review_closed
    FROM live_states state LEFT JOIN live_reviews review USING (video_id)
), live_summary AS (
    SELECT COUNT(video_id) FILTER (WHERE state = 'LIVE') AS live,
           COUNT(video_id) FILTER (WHERE state = 'UPCOMING') AS upcoming,
           COUNT(video_id) FILTER (WHERE state IS NULL OR state NOT IN ('LIVE', 'UPCOMING')) AS other,
           COUNT(video_id) FILTER (WHERE (state IS NOT NULL AND state IS DISTINCT FROM product_state)
               OR (state IS NULL AND (product_state='LIVE' OR (product_state='UPCOMING' AND lifecycle_origin='observed')))) AS state_mismatch,
           COUNT(video_id) FILTER (WHERE state = 'UPCOMING' AND scheduled_start_time < statement_timestamp()) AS past_due,
           COUNT(video_id) FILTER (WHERE state = 'UPCOMING' AND scheduled_start_time < statement_timestamp() - INTERVAL '7 days') AS past_due_7d,
           COUNT(video_id) AS retained_total,
           COUNT(video_id) FILTER(WHERE product_state='UPCOMING' AND lifecycle_origin='metadata_only') AS metadata_only,
           COUNT(video_id) FILTER(WHERE lifecycle_origin='legacy_unknown' AND NOT review_closed) AS legacy_unreviewed,
           COUNT(video_id) FILTER(WHERE review_closed) AS closed_unresolved,
           COUNT(video_id) FILTER(WHERE checked_at IS NULL AND lifecycle_origin='legacy_unknown' AND NOT review_closed) AS never_checked,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp()-checked_at),0)) FILTER(WHERE lifecycle_origin='legacy_unknown' AND NOT review_closed),0)::double precision AS oldest_check_age,
           COUNT(video_id) FILTER(WHERE NOT review_closed AND (lifecycle_origin='legacy_unknown'
               OR (product_state='UPCOMING' AND lifecycle_origin='metadata_only' AND scheduled_start_time<statement_timestamp() AND checked_at IS NOT NULL))) AS unresolved_unreviewed,
           COUNT(video_id) FILTER(WHERE lifecycle_origin='metadata_only' AND checked_at IS NULL) AS metadata_never_checked,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp()-checked_at),0)) FILTER(WHERE lifecycle_origin='metadata_only'),0)::double precision AS metadata_check_age,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp()-reviewed_at),0)) FILTER(WHERE review_closed),0)::double precision AS oldest_review_age
    FROM reviewed_live_states
), target_summary AS (
    SELECT m.kind, EXISTS(SELECT 1 FROM current_projection) AS projection_valid,
           COUNT(s.subject_key) AS targets,
           COUNT(s.subject_key) FILTER (WHERE s.last_completed_at IS NULL AND NOT s.sleeping) AS never_completed,
           COUNT(s.subject_key) FILTER (WHERE s.last_completed_at IS NOT NULL
               AND GREATEST(s.last_completed_at + s.interval_ms * INTERVAL '1 millisecond', s.eligible_at) < statement_timestamp()) AS stale,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - s.last_completed_at), 0)) FILTER (WHERE NOT s.sleeping), 0)::double precision AS oldest_completion_age,
           COUNT(s.subject_key) FILTER (WHERE s.due_at <= statement_timestamp()) AS due,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - s.due_at), 0)), 0)::double precision AS oldest_due_age,
           COALESCE(SUM(s.rpc_calls * 1000.0 / CASE WHEN s.sleeping THEN GREATEST(s.interval_ms, $1::bigint) ELSE s.interval_ms END), 0)::double precision AS required_rpc_rate
    FROM mapping m LEFT JOIN samples s ON s.kind = m.kind
    GROUP BY m.kind
)
SELECT t.kind, t.projection_valid, t.targets, t.never_completed, t.stale,
       t.oldest_completion_age, t.due, t.oldest_due_age, t.required_rpc_rate,
       l.live, l.upcoming, l.other, l.state_mismatch, l.past_due, l.past_due_7d, l.retained_total, l.metadata_only, l.legacy_unreviewed, l.closed_unresolved, l.never_checked, l.oldest_check_age, l.unresolved_unreviewed, l.metadata_never_checked, l.metadata_check_age, l.oldest_review_age
FROM target_summary t CROSS JOIN live_summary l
ORDER BY t.kind;
