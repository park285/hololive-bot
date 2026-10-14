-- 운영 roster의 LIVE와 지난 일정·출처 미상 UPCOMING을 신선도와 무관한 구조 membership으로 읽는다.
-- 현재 원본에 적용되는 검토 영수증(youtube_live_review_current_receipt)이 있는 UPCOMING은 제외한다. UNKNOWN 자체는 대상 선정의 종료 근거가 아니다(해소 불가 종료는 video consumer가 판정한다).
-- 신선도 정책은 API에서 계산한다. 같은 statement의 DB 시각과 필요한 사실 시각만 반환한다.
-- 구조 membership 필터 뒤 상한 판정용 $2행을 읽어 미래 일정 때문에 적격 영상이 잘리지 않게 한다.
WITH clock AS MATERIALIZED (
    SELECT statement_timestamp() AS as_of
), active_sessions AS MATERIALIZED (
    -- 미래 UPCOMING은 영수증과 무관하게 membership 밖이므로 보조 원본을 읽기 전에 제외한다.
    SELECT video_id,channel_id,status,scheduled_start_time,lifecycle_origin,
           title,topic_id,is_premiere,started_at,ended_at,live_first_seen_at
    FROM youtube_live_sessions
    WHERE (status='LIVE' OR status='UPCOMING')
      AND (channel_id IN (SELECT unnest($1::text[]))) IS TRUE
      AND (status='LIVE' OR scheduled_start_time<statement_timestamp()
           OR (lifecycle_origin='legacy_unknown' AND scheduled_start_time IS NULL))
), active_heads AS MATERIALIZED (
    -- 구조 membership에 남은 영상의 head만 한 번 읽어 미래 일정의 head까지 반복해서 읽지 않는다.
    SELECT video_id,status,last_live_positive_at,last_live_positive_seen_at,
           last_upcoming_positive_at,last_upcoming_positive_seen_at,unresolvable_since,
           last_end_evidence_at,last_complete_absence_at,last_absence_scheduled_for,
           first_absence_scheduled_for,second_absence_scheduled_for,consecutive_absence_slots,
           last_absence_observation_id,end_candidate_kind,end_candidate_observation_id,ended_at,end_reason
    FROM youtube_live_reconciliation_heads
    WHERE video_id=ANY(ARRAY(SELECT video_id FROM active_sessions))
), active_availability AS MATERIALIZED (
    SELECT video_id,effective_at,observed_at,availability
    FROM youtube_video_availability
    -- 작은 가용성 표를 한 번 필터링한다. semijoin으로 풀려 영상별 PK 탐색이 되는 것을 피한다.
    WHERE (video_id IN (SELECT video_id FROM active_sessions WHERE status='UPCOMING')) IS TRUE
), review_receipts AS MATERIALIZED (
    SELECT receipt.video_id,receipt.semantic_value
    FROM youtube_live_review_receipt_facts receipt
    WHERE receipt.video_id=ANY(ARRAY(SELECT video_id FROM active_sessions WHERE status='UPCOMING'))
), review_pending AS MATERIALIZED (
    SELECT video_id,channel_id,kind,observation_id,effective_at,scheduled_for,ended_at,
           negative_eligible,scope_covers
    FROM youtube_live_pending_ends
    WHERE video_id=ANY(ARRAY(SELECT DISTINCT video_id FROM review_receipts))
      AND EXISTS (SELECT 1 FROM review_receipts)
), source_join AS (
    -- membership과 검토는 같은 scalar 결합을 재사용하며 원본이나 CTE 조인을 반복하지 않는다.
    SELECT s.video_id,s.channel_id,s.status,s.scheduled_start_time,s.lifecycle_origin,
           s.title,s.topic_id,s.is_premiere,s.started_at,s.ended_at,s.live_first_seen_at,
           h.video_id AS head_video_id,h.status AS head_status,
           h.last_upcoming_positive_at,h.last_upcoming_positive_seen_at,
           h.last_live_positive_at,h.last_live_positive_seen_at,
           h.last_end_evidence_at,h.last_complete_absence_at,h.last_absence_scheduled_for,
           h.first_absence_scheduled_for,h.second_absence_scheduled_for,h.consecutive_absence_slots,
           h.last_absence_observation_id,h.end_candidate_kind,h.end_candidate_observation_id,
           h.ended_at AS head_ended_at,h.end_reason,h.unresolvable_since,
           pending.video_id AS pending_video_id,pending.channel_id AS pending_channel_id,
           pending.kind AS pending_kind,pending.observation_id AS pending_observation_id,
           pending.effective_at AS pending_effective_at,pending.scheduled_for AS pending_scheduled_for,
           pending.ended_at AS pending_ended_at,pending.negative_eligible,pending.scope_covers,
           availability.video_id AS availability_video_id,availability.availability,
           availability.effective_at AS availability_at,availability.observed_at AS availability_seen_at
    FROM active_sessions s
    LEFT JOIN active_heads h ON h.video_id=s.video_id
    LEFT JOIN review_pending pending ON pending.video_id=s.video_id
    LEFT JOIN active_availability availability ON availability.video_id=s.video_id
), source_frame AS MATERIALIZED (
    -- 같은 영상의 여러 영수증도 한 번 만든 typed 값을 재사용한다.
    SELECT s.video_id,s.channel_id,s.status,s.last_live_positive_at,s.last_live_positive_seen_at,
           s.last_upcoming_positive_at,s.last_upcoming_positive_seen_at,s.availability_at,
           s.availability_seen_at,s.unresolvable_since,
           CASE WHEN s.status='UPCOMING' AND s.availability_video_id IS NOT NULL
                     AND (s.video_id IN (SELECT video_id FROM review_receipts)) IS TRUE
                     AND (s.head_video_id IS NULL OR s.head_status=s.status)
                     AND (s.lifecycle_origin<>'observed' OR s.head_video_id IS NOT NULL)
                     AND NOT COALESCE(GREATEST(s.last_upcoming_positive_at,s.last_live_positive_at)>=s.availability_at,false)
                THEN public.youtube_live_review_source_value(
                    s.video_id,s.channel_id,s.status,s.lifecycle_origin,
                    s.title,s.topic_id,s.is_premiere,s.scheduled_start_time,
                    s.started_at,s.ended_at,s.live_first_seen_at,s.head_video_id,
                    s.head_status,s.last_upcoming_positive_at,s.last_upcoming_positive_seen_at,s.last_live_positive_at,
                    s.last_live_positive_seen_at,s.last_end_evidence_at,s.last_complete_absence_at,s.last_absence_scheduled_for,
                    s.first_absence_scheduled_for,s.second_absence_scheduled_for,s.consecutive_absence_slots,s.last_absence_observation_id,
                    s.end_candidate_kind,s.end_candidate_observation_id,s.head_ended_at,s.end_reason,
                    s.pending_video_id,s.pending_channel_id,s.pending_kind,s.pending_observation_id,
                    s.pending_effective_at,s.pending_scheduled_for,s.pending_ended_at,s.negative_eligible,
                    s.scope_covers,s.availability)
           END AS semantic_value
    FROM source_join s
), members AS (
    SELECT s.video_id,s.channel_id,s.status='UPCOMING' AS is_upcoming,
           CASE WHEN s.status='LIVE' THEN s.last_live_positive_at ELSE s.last_upcoming_positive_at END AS positive_at,
           CASE WHEN s.status='LIVE' THEN s.last_live_positive_seen_at ELSE s.last_upcoming_positive_seen_at END AS positive_seen_at,
           CASE WHEN s.status='UPCOMING' THEN s.availability_at END AS availability_at,
           CASE WHEN s.status='UPCOMING' THEN s.availability_seen_at END AS availability_seen_at,
           -- LIVE는 재확인 주기를 늦추지 않는다. UPCOMING도 모든 positive보다 늦게 시작한 추적만 쓴다.
           CASE WHEN s.status='UPCOMING'
                     AND s.unresolvable_since>COALESCE(GREATEST(s.last_upcoming_positive_at,s.last_live_positive_at),'-infinity')
                THEN s.unresolvable_since END AS unresolvable_since
    FROM source_frame s
    LEFT JOIN review_receipts receipt ON receipt.video_id=s.video_id
    GROUP BY s.video_id,s.channel_id,s.status,s.last_live_positive_at,s.last_live_positive_seen_at,
             s.last_upcoming_positive_at,s.last_upcoming_positive_seen_at,s.availability_at,
             s.availability_seen_at,s.unresolvable_since
    HAVING s.status='LIVE' OR NOT COALESCE(bool_or(receipt.semantic_value=s.semantic_value),false)
)
SELECT video_id,channel_id,is_upcoming,clock.as_of,
       positive_at,positive_seen_at,availability_at,availability_seen_at,unresolvable_since
FROM members CROSS JOIN clock
ORDER BY video_id COLLATE "C"
LIMIT $2
