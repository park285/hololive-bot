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
    -- 내부 식별자는 bytewise로 묶어 locale 문자열 정렬 비용을 피한다. 최종 표시 순서는 별도로 보존한다.
    SELECT m.kind COLLATE "C" AS kind, CASE WHEN m.rpc_per_kind THEN COUNT(t.observation_kind) ELSE 1 END AS rpc_calls,
           t.subject_key COLLATE "C" AS subject_key,
           MIN(t.poll_interval_ms) AS interval_ms, MIN(t.created_at) AS created_at,
           -- bundle 안 한 row라도 확인 가능하면 신규 admission 대상이다.
           MIN(COALESCE(t.not_before, '-infinity'::timestamptz)) AS eligible_at
    FROM mapped_observations m
    JOIN youtube_collection_targets t ON t.observation_kind = m.observation_kind
    JOIN current_projection g ON g.generation = t.projection_generation
    WHERE t.enabled
    GROUP BY m.kind COLLATE "C", m.rpc_per_kind, t.subject_key COLLATE "C"
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
), active_session_sources AS MATERIALIZED (
    SELECT video_id,channel_id,status,scheduled_start_time,lifecycle_origin,
           title,topic_id,is_premiere,started_at,ended_at,live_first_seen_at
    FROM youtube_live_sessions
    WHERE status IN ('LIVE','UPCOMING')
), active_head_sources AS MATERIALIZED (
    SELECT video_id,status,last_upcoming_positive_at,last_upcoming_positive_seen_at,
           last_live_positive_at,last_live_positive_seen_at,last_end_evidence_at,last_complete_absence_at,
           last_absence_scheduled_for,first_absence_scheduled_for,second_absence_scheduled_for,
           consecutive_absence_slots,last_absence_observation_id,end_candidate_kind,
           end_candidate_observation_id,ended_at,end_reason
    FROM youtube_live_reconciliation_heads
    WHERE status IN ('LIVE','UPCOMING')
), inactive_live_products AS MATERIALIZED (
    -- 활성 canonical은 다시 읽지 않는다. head만 활성인 ENDED canonical도 불일치 집계에 남긴다.
    SELECT video_id,channel_id,status,scheduled_start_time,lifecycle_origin,
           title,topic_id,is_premiere,started_at,ended_at,live_first_seen_at
    FROM youtube_live_sessions
    WHERE video_id=ANY(ARRAY(
        SELECT video_id FROM active_head_sources
        EXCEPT SELECT video_id FROM active_session_sources))
), active_live_heads AS MATERIALIZED (
    -- canonical만 활성인 경우의 ENDED head를 보충하되 활성 head는 재탐색하지 않는다.
    SELECT video_id,status,last_upcoming_positive_at,last_upcoming_positive_seen_at,
           last_live_positive_at,last_live_positive_seen_at,last_end_evidence_at,last_complete_absence_at,
           last_absence_scheduled_for,first_absence_scheduled_for,second_absence_scheduled_for,
           consecutive_absence_slots,last_absence_observation_id,end_candidate_kind,
           end_candidate_observation_id,ended_at,end_reason
    FROM active_head_sources
    UNION ALL
    SELECT video_id,status,last_upcoming_positive_at,last_upcoming_positive_seen_at,
           last_live_positive_at,last_live_positive_seen_at,last_end_evidence_at,last_complete_absence_at,
           last_absence_scheduled_for,first_absence_scheduled_for,second_absence_scheduled_for,
           consecutive_absence_slots,last_absence_observation_id,end_candidate_kind,
           end_candidate_observation_id,ended_at,end_reason
    FROM youtube_live_reconciliation_heads
    WHERE video_id=ANY(ARRAY(
        SELECT video_id FROM active_session_sources
        EXCEPT SELECT video_id FROM active_head_sources))
), active_live_availability AS MATERIALIZED (
    SELECT video_id,observed_at,effective_at,availability
    FROM youtube_video_availability
    -- 관계 집합의 실제 추정치를 유지해 대규모 활성 집합을 배열 기본 추정치로 축소하지 않는다.
    WHERE video_id IN (
        SELECT video_id FROM active_session_sources
        UNION ALL SELECT video_id FROM inactive_live_products
    )
), live_states AS MATERIALIZED (
    -- unique ID 통계가 있는 활성 canonical을 먼저 결합하고 완성된 프레임만 합친다.
    -- 양쪽을 UNION한 뒤 조인하면 distinct 추정치가 사라져 대규모 입력의 비용이 제곱으로 부풀 수 있다.
    SELECT product.video_id,product.channel_id,product.status,product.scheduled_start_time,product.lifecycle_origin,
           product.title,product.topic_id,product.is_premiere,product.started_at,product.ended_at,product.live_first_seen_at,
           h.video_id AS head_video_id,h.status AS state,product.status AS product_state,
           h.last_upcoming_positive_at,h.last_upcoming_positive_seen_at,
           h.last_live_positive_at,h.last_live_positive_seen_at,
           h.last_end_evidence_at,h.last_complete_absence_at,h.last_absence_scheduled_for,
           h.first_absence_scheduled_for,h.second_absence_scheduled_for,h.consecutive_absence_slots,
           h.last_absence_observation_id,h.end_candidate_kind,h.end_candidate_observation_id,
           h.ended_at AS head_ended_at,h.end_reason,
           availability.video_id AS availability_video_id,availability.availability,
           availability.effective_at AS availability_at,availability.observed_at AS checked_at
    FROM active_session_sources product
    LEFT JOIN active_live_heads h ON h.video_id = product.video_id
    LEFT JOIN active_live_availability availability ON availability.video_id = product.video_id
    UNION ALL
    SELECT product.video_id,product.channel_id,product.status,product.scheduled_start_time,product.lifecycle_origin,
           product.title,product.topic_id,product.is_premiere,product.started_at,product.ended_at,product.live_first_seen_at,
           h.video_id AS head_video_id,h.status AS state,product.status AS product_state,
           h.last_upcoming_positive_at,h.last_upcoming_positive_seen_at,
           h.last_live_positive_at,h.last_live_positive_seen_at,
           h.last_end_evidence_at,h.last_complete_absence_at,h.last_absence_scheduled_for,
           h.first_absence_scheduled_for,h.second_absence_scheduled_for,h.consecutive_absence_slots,
           h.last_absence_observation_id,h.end_candidate_kind,h.end_candidate_observation_id,
           h.ended_at AS head_ended_at,h.end_reason,
           availability.video_id AS availability_video_id,availability.availability,
           availability.effective_at AS availability_at,availability.observed_at AS checked_at
    FROM inactive_live_products product
    JOIN active_head_sources h ON h.video_id=product.video_id
    LEFT JOIN active_live_availability availability ON availability.video_id=product.video_id
), live_review_receipts AS MATERIALIZED (
    SELECT receipt.video_id,receipt.recorded_at,receipt.semantic_value
    FROM youtube_live_review_receipt_facts receipt
    WHERE receipt.video_id=ANY(ARRAY(SELECT video_id FROM live_states))
), live_review_pending AS MATERIALIZED (
    SELECT video_id,channel_id,kind,observation_id,effective_at,scheduled_for,ended_at,
           negative_eligible,scope_covers
    FROM youtube_live_pending_ends
    WHERE video_id=ANY(ARRAY(SELECT DISTINCT video_id FROM live_review_receipts))
      AND EXISTS (SELECT 1 FROM live_review_receipts)
), live_review_facts AS MATERIALIZED (
    -- 영수증 후보의 typed 값을 한 번만 만든다. NULL 속성·누락 object도 named composite 동등성으로 비교한다.
    SELECT s.video_id,public.youtube_live_review_source_value(
           s.video_id,s.channel_id,s.status,s.lifecycle_origin,
           s.title,s.topic_id,s.is_premiere,s.scheduled_start_time,
           s.started_at,s.ended_at,s.live_first_seen_at,s.head_video_id,
           s.state,s.last_upcoming_positive_at,s.last_upcoming_positive_seen_at,s.last_live_positive_at,
           s.last_live_positive_seen_at,s.last_end_evidence_at,s.last_complete_absence_at,s.last_absence_scheduled_for,
           s.first_absence_scheduled_for,s.second_absence_scheduled_for,s.consecutive_absence_slots,s.last_absence_observation_id,
           s.end_candidate_kind,s.end_candidate_observation_id,s.head_ended_at,s.end_reason,
           pending.video_id,pending.channel_id,pending.kind,pending.observation_id,
           pending.effective_at,pending.scheduled_for,pending.ended_at,pending.negative_eligible,
           pending.scope_covers,s.availability) AS semantic_value
    FROM live_states s
    LEFT JOIN live_review_pending pending ON pending.video_id=s.video_id
    WHERE s.status='UPCOMING' AND s.availability_video_id IS NOT NULL
      AND (s.video_id IN (SELECT video_id FROM live_review_receipts)) IS TRUE
      AND (s.head_video_id IS NULL OR s.state=s.status)
      AND (s.lifecycle_origin<>'observed' OR s.head_video_id IS NOT NULL)
      AND NOT COALESCE(GREATEST(s.last_upcoming_positive_at,s.last_live_positive_at)>=s.availability_at,false)
), live_reviews AS MATERIALIZED (
    SELECT current.video_id,max(receipt.recorded_at) AS reviewed_at
    FROM live_review_facts current
    JOIN live_review_receipts receipt ON receipt.video_id=current.video_id AND receipt.semantic_value=current.semantic_value
    GROUP BY current.video_id
), reviewed_live_states AS MATERIALIZED (
    -- 영수증까지 결합한 뒤 채널 membership을 적용해 작은 추정치의 반복 조인을 피한다.
    -- recorded_at은 NOT NULL이므로 현재 원본에 적용되는 영수증 존재와 같은 판정입니다.
    SELECT state.video_id, state.channel_id, state.state, state.product_state, state.scheduled_start_time,
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
           -- 가용성 확인이 없는 미래 일정 legacy_unknown은 아직 확인할 수 없어 legacy_unreviewed·legacy_not_due로만 남깁니다.
           COUNT(video_id) FILTER(WHERE NOT review_closed AND ((lifecycle_origin='legacy_unknown'
                   AND NOT COALESCE(product_state='UPCOMING' AND scheduled_start_time>=statement_timestamp(),false))
               OR (product_state='UPCOMING' AND lifecycle_origin='metadata_only' AND scheduled_start_time<statement_timestamp() AND checked_at IS NOT NULL))) AS unresolved_unreviewed,
           COUNT(video_id) FILTER(WHERE NOT review_closed AND lifecycle_origin='legacy_unknown'
               AND product_state='UPCOMING' AND scheduled_start_time>=statement_timestamp()) AS legacy_not_due,
           COUNT(video_id) FILTER(WHERE lifecycle_origin='metadata_only' AND checked_at IS NULL) AS metadata_never_checked,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp()-checked_at),0)) FILTER(WHERE lifecycle_origin='metadata_only'),0)::double precision AS metadata_check_age,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp()-reviewed_at),0)) FILTER(WHERE review_closed),0)::double precision AS oldest_review_age
    FROM reviewed_live_states
    WHERE channel_id IN (
        SELECT subject_key FROM targets WHERE kind='youtubejs_channel_live'
    )
), target_summary AS (
    SELECT m.kind COLLATE "C" AS kind, EXISTS(SELECT 1 FROM current_projection) AS projection_valid,
           COUNT(s.subject_key) AS targets,
           COUNT(s.subject_key) FILTER (WHERE s.last_completed_at IS NULL AND NOT s.sleeping) AS never_completed,
           COUNT(s.subject_key) FILTER (WHERE s.last_completed_at IS NOT NULL
               AND GREATEST(s.last_completed_at + s.interval_ms * INTERVAL '1 millisecond', s.eligible_at) < statement_timestamp()) AS stale,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - s.last_completed_at), 0)) FILTER (WHERE NOT s.sleeping), 0)::double precision AS oldest_completion_age,
           COUNT(s.subject_key) FILTER (WHERE s.due_at <= statement_timestamp()) AS due,
           COALESCE(MAX(GREATEST(EXTRACT(EPOCH FROM statement_timestamp() - s.due_at), 0)), 0)::double precision AS oldest_due_age,
           COALESCE(SUM(s.rpc_calls * 1000.0 / CASE WHEN s.sleeping THEN GREATEST(s.interval_ms, $1::bigint) ELSE s.interval_ms END), 0)::double precision AS required_rpc_rate
    FROM mapping m LEFT JOIN samples s ON s.kind = m.kind
    GROUP BY m.kind COLLATE "C"
)
SELECT t.kind, t.projection_valid, t.targets, t.never_completed, t.stale,
       t.oldest_completion_age, t.due, t.oldest_due_age, t.required_rpc_rate,
       l.live, l.upcoming, l.other, l.state_mismatch, l.past_due, l.past_due_7d, l.retained_total, l.metadata_only, l.legacy_unreviewed, l.closed_unresolved, l.never_checked, l.oldest_check_age, l.unresolved_unreviewed, l.legacy_not_due, l.metadata_never_checked, l.metadata_check_age, l.oldest_review_age
FROM target_summary t CROSS JOIN live_summary l
ORDER BY t.kind COLLATE "default";
