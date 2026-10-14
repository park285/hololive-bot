-- rpc_per_kind: 활성 kind마다 helper RPC를 따로 보내는 job(content 목록 2종)이다.
-- 나머지 job은 bundle 한 번에 RPC 1회를 보낸다. 방송 탭 snapshot과 채널 /live 확인은 별도 job·슬롯이다.
-- 수요는 기본 cadence의 명목값이며 재시도와 job 안의 추가 보강 호출은 포함하지 않는다.
-- $1은 기존 LIVE 신선도 예산(ms)이다. not_before가 미래인 bundle은 신선한 근거로 잠든 대상이라
-- due·stale·미완료·완료 나이에서 깨어날 때까지 빠지고, 수요는 예산당 최대 1회로 센다.
-- 미획득 target의 due 기준은 연속 membership 동안 보존되는 created_at이다.
WITH mapped_observations(kind, observation_kind, rpc_per_kind) AS (
    VALUES
        ('youtubejs_channel_live', 'live_snapshot', FALSE),
        ('youtubejs_channel_live_check', 'channel_live_check', FALSE),
        ('youtubejs_video_live', 'video_live_check', FALSE),
        ('community_collect', 'community_page', FALSE),
        ('youtubejs_content', 'video_list', TRUE),
        ('youtubejs_content', 'shorts_list', TRUE),
        ('youtubejs_channel_metadata', 'channel_profile', FALSE),
        ('youtubejs_channel_metadata', 'channel_photo', FALSE)
), mapping AS (
    SELECT DISTINCT kind,rpc_per_kind FROM mapped_observations
), current_projection AS (
    SELECT generation FROM youtube_collection_projection_generations
    WHERE status = 'CURRENT' AND valid_until > statement_timestamp()
), targets AS (
    -- 내부 식별자는 bytewise로 묶되 최종 출력 순서는 DB 기본 collation을 유지한다.
    SELECT m.kind COLLATE "C" AS kind,CASE WHEN m.rpc_per_kind THEN COUNT(t.observation_kind) ELSE 1 END AS rpc_calls,
           t.subject_key COLLATE "C" AS subject_key,
           MIN(t.poll_interval_ms) AS interval_ms,MIN(t.created_at) AS created_at,
           MIN(COALESCE(t.not_before,'-infinity'::timestamptz)) AS eligible_at
    FROM mapped_observations m
    JOIN youtube_collection_targets t ON t.observation_kind=m.observation_kind
    JOIN current_projection g ON g.generation=t.projection_generation
    WHERE t.enabled
    GROUP BY m.kind COLLATE "C",m.rpc_per_kind,t.subject_key COLLATE "C"
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
), active_session_sources AS (
    SELECT video_id,channel_id,status,scheduled_start_time,lifecycle_origin,
           title,topic_id,is_premiere,started_at,ended_at,live_first_seen_at
    FROM youtube_live_sessions
    WHERE status IN ('LIVE','UPCOMING')
), active_head_sources AS (
    SELECT video_id,status,last_upcoming_positive_at,last_upcoming_positive_seen_at,
           last_live_positive_at,last_live_positive_seen_at,last_end_evidence_at,last_complete_absence_at,
           last_absence_scheduled_for,first_absence_scheduled_for,second_absence_scheduled_for,
           consecutive_absence_slots,last_absence_observation_id,end_candidate_kind,
           end_candidate_observation_id,ended_at,end_reason
    FROM youtube_live_reconciliation_heads
    WHERE status IN ('LIVE','UPCOMING')
), active_pairs AS MATERIALIZED (
    -- 두 PK 원본을 먼저 결합해 큰 활성 집합에서도 고유값 수 통계를 보존한다.
    SELECT COALESCE(session.video_id,head.video_id) AS video_id,
           session.video_id AS session_video_id,
           session.channel_id AS session_channel_id,
           session.status AS session_status,
           session.lifecycle_origin AS session_lifecycle_origin,
           session.title AS session_title,
           session.topic_id AS session_topic_id,
           session.is_premiere AS session_is_premiere,
           session.scheduled_start_time AS session_scheduled_start_time,
           session.started_at AS session_started_at,
           session.ended_at AS session_ended_at,
           session.live_first_seen_at AS session_live_first_seen_at,
           head.video_id AS head_video_id,
           head.status AS head_status,
           head.last_upcoming_positive_at AS head_last_upcoming_positive_at,
           head.last_upcoming_positive_seen_at AS head_last_upcoming_positive_seen_at,
           head.last_live_positive_at AS head_last_live_positive_at,
           head.last_live_positive_seen_at AS head_last_live_positive_seen_at,
           head.last_end_evidence_at AS head_last_end_evidence_at,
           head.last_complete_absence_at AS head_last_complete_absence_at,
           head.last_absence_scheduled_for AS head_last_absence_scheduled_for,
           head.first_absence_scheduled_for AS head_first_absence_scheduled_for,
           head.second_absence_scheduled_for AS head_second_absence_scheduled_for,
           head.consecutive_absence_slots AS head_consecutive_absence_slots,
           head.last_absence_observation_id AS head_last_absence_observation_id,
           head.end_candidate_kind AS head_end_candidate_kind,
           head.end_candidate_observation_id AS head_end_candidate_observation_id,
           head.ended_at AS head_ended_at,
           head.end_reason AS head_end_reason
    FROM active_session_sources session
    FULL JOIN active_head_sources head ON head.video_id=session.video_id
), missing_sessions AS MATERIALIZED (
    -- head만 활성인 영상의 ENDED canonical도 불일치 집계에 남긴다.
    SELECT video_id,channel_id,status,lifecycle_origin,title,topic_id,is_premiere,
           scheduled_start_time,started_at,ended_at,live_first_seen_at
    FROM youtube_live_sessions
    WHERE video_id=ANY(ARRAY(SELECT video_id FROM active_pairs WHERE session_video_id IS NULL))
), missing_heads AS MATERIALIZED (
    -- canonical만 활성인 영상도 ENDED head의 모순을 숨기지 않는다.
    SELECT video_id,status,last_upcoming_positive_at,last_upcoming_positive_seen_at,
           last_live_positive_at,last_live_positive_seen_at,last_end_evidence_at,last_complete_absence_at,
           last_absence_scheduled_for,first_absence_scheduled_for,second_absence_scheduled_for,
           consecutive_absence_slots,last_absence_observation_id,end_candidate_kind,
           end_candidate_observation_id,ended_at,end_reason
    FROM youtube_live_reconciliation_heads
    WHERE video_id=ANY(ARRAY(SELECT video_id FROM active_pairs WHERE head_video_id IS NULL))
), active_live_availability AS MATERIALIZED (
    SELECT video_id,observed_at,effective_at,availability
    FROM youtube_video_availability
    WHERE (video_id IN (SELECT video_id FROM active_pairs)) IS TRUE
), live_review_receipts AS MATERIALIZED (
    SELECT receipt.video_id,receipt.recorded_at,receipt.semantic_value
    FROM youtube_live_review_receipt_facts receipt
    WHERE receipt.video_id=ANY(ARRAY(SELECT video_id FROM active_pairs))
), live_review_pending AS MATERIALIZED (
    SELECT video_id,channel_id,kind,observation_id,effective_at,scheduled_for,ended_at,
           negative_eligible,scope_covers
    FROM youtube_live_pending_ends
    WHERE video_id=ANY(ARRAY(SELECT DISTINCT video_id FROM live_review_receipts))
      AND EXISTS (SELECT 1 FROM live_review_receipts)
), source_join AS (
    -- 보충 행은 원본 행 전체가 없던 쪽에만 붙인다. 따라서 NULL 가능 열의 COALESCE도 원본 값을 바꾸지 않는다.
    SELECT COALESCE(pair.session_video_id,missing_session.video_id) AS video_id,
           COALESCE(pair.session_channel_id,missing_session.channel_id) AS channel_id,
           COALESCE(pair.session_status,missing_session.status) AS status,
           COALESCE(pair.session_lifecycle_origin,missing_session.lifecycle_origin) AS lifecycle_origin,
           COALESCE(pair.session_title,missing_session.title) AS title,
           COALESCE(pair.session_topic_id,missing_session.topic_id) AS topic_id,
           COALESCE(pair.session_is_premiere,missing_session.is_premiere) AS is_premiere,
           COALESCE(pair.session_scheduled_start_time,missing_session.scheduled_start_time) AS scheduled_start_time,
           COALESCE(pair.session_started_at,missing_session.started_at) AS started_at,
           COALESCE(pair.session_ended_at,missing_session.ended_at) AS ended_at,
           COALESCE(pair.session_live_first_seen_at,missing_session.live_first_seen_at) AS live_first_seen_at,
           COALESCE(pair.head_video_id,missing_head.video_id) AS head_video_id,
           COALESCE(pair.head_status,missing_head.status) AS state,
           COALESCE(pair.head_last_upcoming_positive_at,missing_head.last_upcoming_positive_at) AS last_upcoming_positive_at,
           COALESCE(pair.head_last_upcoming_positive_seen_at,missing_head.last_upcoming_positive_seen_at) AS last_upcoming_positive_seen_at,
           COALESCE(pair.head_last_live_positive_at,missing_head.last_live_positive_at) AS last_live_positive_at,
           COALESCE(pair.head_last_live_positive_seen_at,missing_head.last_live_positive_seen_at) AS last_live_positive_seen_at,
           COALESCE(pair.head_last_end_evidence_at,missing_head.last_end_evidence_at) AS last_end_evidence_at,
           COALESCE(pair.head_last_complete_absence_at,missing_head.last_complete_absence_at) AS last_complete_absence_at,
           COALESCE(pair.head_last_absence_scheduled_for,missing_head.last_absence_scheduled_for) AS last_absence_scheduled_for,
           COALESCE(pair.head_first_absence_scheduled_for,missing_head.first_absence_scheduled_for) AS first_absence_scheduled_for,
           COALESCE(pair.head_second_absence_scheduled_for,missing_head.second_absence_scheduled_for) AS second_absence_scheduled_for,
           COALESCE(pair.head_consecutive_absence_slots,missing_head.consecutive_absence_slots) AS consecutive_absence_slots,
           COALESCE(pair.head_last_absence_observation_id,missing_head.last_absence_observation_id) AS last_absence_observation_id,
           COALESCE(pair.head_end_candidate_kind,missing_head.end_candidate_kind) AS end_candidate_kind,
           COALESCE(pair.head_end_candidate_observation_id,missing_head.end_candidate_observation_id) AS end_candidate_observation_id,
           COALESCE(pair.head_ended_at,missing_head.ended_at) AS head_ended_at,
           COALESCE(pair.head_end_reason,missing_head.end_reason) AS end_reason,
           COALESCE(pair.session_status,missing_session.status) AS product_state,
           availability.video_id AS availability_video_id,
           availability.availability,
           availability.effective_at AS availability_at,
           availability.observed_at AS checked_at
    FROM active_pairs pair
    LEFT JOIN missing_sessions missing_session ON pair.session_video_id IS NULL AND missing_session.video_id=pair.video_id
    LEFT JOIN missing_heads missing_head ON pair.head_video_id IS NULL AND missing_head.video_id=pair.video_id
    LEFT JOIN active_live_availability availability ON availability.video_id=pair.video_id
    WHERE COALESCE(pair.session_video_id,missing_session.video_id) IS NOT NULL
), live_states AS MATERIALIZED (
    -- 검토 후보의 typed 사실은 한 번만 만들고 집계용 프레임에는 필요한 열만 유지한다.
    SELECT s.video_id,s.channel_id,s.state,s.product_state,s.scheduled_start_time,s.lifecycle_origin,s.checked_at,
           CASE
               WHEN s.status='UPCOMING' AND s.availability_video_id IS NOT NULL
                 AND (s.video_id IN (SELECT video_id FROM live_review_receipts)) IS TRUE
                 AND (s.head_video_id IS NULL OR s.state=s.status)
                 AND (s.lifecycle_origin<>'observed' OR s.head_video_id IS NOT NULL)
                 AND NOT COALESCE(GREATEST(s.last_upcoming_positive_at,s.last_live_positive_at)>=s.availability_at,false)
               THEN public.youtube_live_review_source_value(
                   s.video_id,s.channel_id,s.status,s.lifecycle_origin,
                   s.title,s.topic_id,s.is_premiere,s.scheduled_start_time,
                   s.started_at,s.ended_at,s.live_first_seen_at,s.head_video_id,
                   s.state,s.last_upcoming_positive_at,s.last_upcoming_positive_seen_at,s.last_live_positive_at,
                   s.last_live_positive_seen_at,s.last_end_evidence_at,s.last_complete_absence_at,s.last_absence_scheduled_for,
                   s.first_absence_scheduled_for,s.second_absence_scheduled_for,s.consecutive_absence_slots,s.last_absence_observation_id,
                   s.end_candidate_kind,s.end_candidate_observation_id,s.head_ended_at,s.end_reason,
                   pending.video_id,pending.channel_id,pending.kind,pending.observation_id,
                   pending.effective_at,pending.scheduled_for,pending.ended_at,pending.negative_eligible,
                   pending.scope_covers,s.availability)
           END AS semantic_value
    FROM source_join s
    LEFT JOIN live_review_pending pending ON pending.video_id=s.video_id
), reviewed_live_states AS (
    -- 동일 영상의 모든 영수증을 같은 typed 값과 비교하고 일치한 최신 recorded_at만 쓴다.
    SELECT state.video_id,state.channel_id,state.state,state.product_state,state.scheduled_start_time,
           state.lifecycle_origin,state.checked_at,
           max(receipt.recorded_at) FILTER (WHERE receipt.semantic_value=state.semantic_value) AS reviewed_at,
           max(receipt.recorded_at) FILTER (WHERE receipt.semantic_value=state.semantic_value) IS NOT NULL AS review_closed
    FROM live_states state
    LEFT JOIN live_review_receipts receipt ON receipt.video_id=state.video_id
    GROUP BY state.video_id,state.channel_id,state.state,state.product_state,state.scheduled_start_time,
             state.lifecycle_origin,state.checked_at
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
