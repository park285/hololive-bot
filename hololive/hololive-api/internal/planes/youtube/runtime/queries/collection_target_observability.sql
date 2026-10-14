-- rpc_per_kind: 활성 kind마다 helper RPC를 따로 보내는 job(content 목록 2종)이다.
-- 나머지 job은 bundle 한 번에 RPC 1회를 보낸다. 방송 탭 snapshot과 채널 /live 확인은 별도 job·슬롯이다.
-- 수요는 기본 cadence의 명목값이며 재시도와 job 안의 추가 보강 호출은 포함하지 않는다.
-- $1은 기존 LIVE 신선도 예산(ms)이다. not_before가 미래인 bundle은 신선한 근거로 잠든 대상이라
-- due·stale·미완료·완료 나이에서 깨어날 때까지 빠지고, 수요는 예산당 최대 1회로 센다.
-- 미획득 target의 due 기준은 연속 membership 동안 보존되는 created_at이다.
WITH mapping AS (
    SELECT DISTINCT kind,rpc_per_kind FROM youtube_collection_runtime_kinds
), current_projection AS (
    SELECT generation FROM youtube_collection_projection_generations
    WHERE status = 'CURRENT' AND valid_until > statement_timestamp()
), targets AS (
    -- 원본 target과 같은 트랜잭션에서 집계한 구조만 읽는다. 세대 만료·lease 시각은 지금 판정한다.
    SELECT bundle.kind COLLATE "C" AS kind,bundle.rpc_calls,
           bundle.subject_key COLLATE "C" AS subject_key,bundle.interval_ms,bundle.created_at,bundle.eligible_at
    FROM youtube_collection_runtime_bundles bundle
    JOIN current_projection generation ON generation.generation=bundle.projection_generation
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
), samples AS MATERIALIZED (
    SELECT kind, rpc_calls, subject_key, interval_ms, eligible_at, last_completed_at,
           GREATEST(lease_due_at, eligible_at) AS due_at,
           eligible_at > statement_timestamp() AS sleeping
    FROM leased
), reviewed_live_states AS (
    -- 쓰기 트랜잭션과 함께 확정한 판정만 읽고 시간 경과에 따른 나이는 조회 시각으로 계산한다.
    SELECT video_id,channel_id,head_status AS state,session_status AS product_state,
           scheduled_start_time,lifecycle_origin,availability_seen_at AS checked_at,
           reviewed_at,reviewed_at IS NOT NULL AS review_closed
    FROM youtube_live_runtime_state
    WHERE channel_id IS NOT NULL
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
           COALESCE(GREATEST(EXTRACT(EPOCH FROM statement_timestamp()-MIN(checked_at) FILTER(WHERE lifecycle_origin='legacy_unknown' AND NOT review_closed)),0),0)::double precision AS oldest_check_age,
           -- 가용성 확인이 없는 미래 일정 legacy_unknown은 아직 확인할 수 없어 legacy_unreviewed·legacy_not_due로만 남깁니다.
           COUNT(video_id) FILTER(WHERE NOT review_closed AND ((lifecycle_origin='legacy_unknown'
                   AND NOT COALESCE(product_state='UPCOMING' AND scheduled_start_time>=statement_timestamp(),false))
               OR (product_state='UPCOMING' AND lifecycle_origin='metadata_only' AND scheduled_start_time<statement_timestamp() AND checked_at IS NOT NULL))) AS unresolved_unreviewed,
           COUNT(video_id) FILTER(WHERE NOT review_closed AND lifecycle_origin='legacy_unknown'
               AND product_state='UPCOMING' AND scheduled_start_time>=statement_timestamp()) AS legacy_not_due,
           COUNT(video_id) FILTER(WHERE lifecycle_origin='metadata_only' AND checked_at IS NULL) AS metadata_never_checked,
           COALESCE(GREATEST(EXTRACT(EPOCH FROM statement_timestamp()-MIN(checked_at) FILTER(WHERE lifecycle_origin='metadata_only')),0),0)::double precision AS metadata_check_age,
           COALESCE(GREATEST(EXTRACT(EPOCH FROM statement_timestamp()-MIN(reviewed_at) FILTER(WHERE review_closed)),0),0)::double precision AS oldest_review_age
    FROM reviewed_live_states
    WHERE (channel_id IN (
        SELECT subject_key FROM targets WHERE kind='youtubejs_channel_live'
    )) IS TRUE
), target_summary AS (
    SELECT m.kind COLLATE "C" AS kind, EXISTS(SELECT 1 FROM current_projection) AS projection_valid,
           COUNT(s.subject_key) AS targets,
           COUNT(s.subject_key) FILTER (WHERE s.last_completed_at IS NULL AND NOT s.sleeping) AS never_completed,
           COUNT(s.subject_key) FILTER (WHERE s.last_completed_at IS NOT NULL
               AND GREATEST(s.last_completed_at + s.interval_ms * INTERVAL '1 millisecond', s.eligible_at) < statement_timestamp()) AS stale,
           COALESCE(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - MIN(s.last_completed_at) FILTER (WHERE NOT s.sleeping)), 0), 0)::double precision AS oldest_completion_age,
           COUNT(s.subject_key) FILTER (WHERE s.due_at <= statement_timestamp()) AS due,
           COALESCE(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - MIN(s.due_at)), 0), 0)::double precision AS oldest_due_age,
           COALESCE(SUM(s.rpc_calls * 1000.0 / CASE WHEN s.sleeping THEN GREATEST(s.interval_ms, $1::bigint) ELSE s.interval_ms END), 0)::double precision AS required_rpc_rate
    FROM mapping m LEFT JOIN samples s ON s.kind = m.kind
    GROUP BY m.kind COLLATE "C"
)
SELECT t.kind, t.projection_valid, t.targets, t.never_completed, t.stale,
       t.oldest_completion_age, t.due, t.oldest_due_age, t.required_rpc_rate,
       l.live, l.upcoming, l.other, l.state_mismatch, l.past_due, l.past_due_7d, l.retained_total, l.metadata_only, l.legacy_unreviewed, l.closed_unresolved, l.never_checked, l.oldest_check_age, l.unresolved_unreviewed, l.legacy_not_due, l.metadata_never_checked, l.metadata_check_age, l.oldest_review_age
FROM target_summary t CROSS JOIN live_summary l
ORDER BY t.kind COLLATE "default";
