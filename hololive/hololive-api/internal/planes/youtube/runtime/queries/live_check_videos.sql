-- 운영 roster의 LIVE와 지난 일정·출처 미상 UPCOMING을 신선도와 무관한 구조 membership으로 읽는다.
-- 현재 원본에 적용되는 검토 영수증(youtube_live_review_current_receipt)이 있는 UPCOMING은 제외한다. UNKNOWN 자체는 대상 선정의 종료 근거가 아니다(해소 불가 종료는 video consumer가 판정한다).
-- 신선도 정책은 API에서 계산한다. 같은 statement의 DB 시각과 필요한 사실 시각만 반환한다.
-- 구조 membership 필터 뒤 상한 판정용 $2행을 읽어 미래 일정 때문에 적격 영상이 잘리지 않게 한다.
WITH clock AS MATERIALIZED (
    SELECT statement_timestamp() AS as_of
), active_sessions AS MATERIALIZED (
    -- 채널의 과거 ENDED를 읽지 않고 활성 원본을 membership과 검토 입력에 함께 씁니다.
    SELECT video_id,channel_id,status,scheduled_start_time,lifecycle_origin
    FROM youtube_live_sessions
    WHERE status IN ('LIVE','UPCOMING') AND channel_id=ANY($1::text[])
), active_heads AS MATERIALIZED (
    -- generic plan의 적은 채널 수 추정이 영상별 PK loop로 번지지 않게 입력을 먼저 고정합니다.
    SELECT video_id,last_live_positive_at,last_live_positive_seen_at,
           last_upcoming_positive_at,last_upcoming_positive_seen_at,unresolvable_since
    FROM youtube_live_reconciliation_heads
    WHERE video_id=ANY(ARRAY(SELECT video_id FROM active_sessions))
), active_availability AS MATERIALIZED (
    SELECT video_id,effective_at,observed_at
    FROM youtube_video_availability
    WHERE video_id=ANY(ARRAY(SELECT video_id FROM active_sessions WHERE status='UPCOMING'))
), review_videos AS MATERIALIZED (
    SELECT DISTINCT receipt.video_id
    FROM youtube_live_review_receipt_facts receipt
    WHERE receipt.video_id=ANY(ARRAY(SELECT video_id FROM active_sessions WHERE status='UPCOMING'))
), review_input AS MATERIALIZED (
    -- 배열 인수를 관계 열로 넘겨 SQL 집합 함수의 인라이닝과 단일 계획을 유지합니다.
    SELECT ARRAY(SELECT video_id FROM review_videos) AS video_ids
), reviewed_videos AS MATERIALIZED (
    -- 검토 후보 집합을 한 번 비교하며 의미 사실이 바뀐 영상은 다시 확인 대상입니다.
    SELECT review.video_id
    FROM review_input input
    CROSS JOIN LATERAL youtube_live_review_current_receipts(input.video_ids) review
), members AS (
    SELECT s.video_id,s.channel_id,FALSE AS is_upcoming,
           h.last_live_positive_at AS positive_at,
           h.last_live_positive_seen_at AS positive_seen_at,
           NULL::timestamptz AS availability_at,
           NULL::timestamptz AS availability_seen_at,
           -- LIVE는 해소 불가 추적이 10분 안에 종료로 끝나므로 재확인 주기를 늦추지 않는다.
           NULL::timestamptz AS unresolvable_since
    FROM active_sessions s CROSS JOIN clock
    LEFT JOIN active_heads h ON h.video_id=s.video_id
    WHERE s.status='LIVE'
    UNION ALL
    SELECT s.video_id,s.channel_id,TRUE AS is_upcoming,
           h.last_upcoming_positive_at AS positive_at,
           h.last_upcoming_positive_seen_at AS positive_seen_at,
           availability.effective_at AS availability_at,
           availability.observed_at AS availability_seen_at,
           -- 추적은 모든 positive보다 늦게 시작된다. positive보다 이른 잔여값(이전 바이너리 기간)은 주기 근거가 아니다.
           CASE WHEN h.unresolvable_since > COALESCE(GREATEST(h.last_upcoming_positive_at, h.last_live_positive_at), '-infinity')
                THEN h.unresolvable_since END AS unresolvable_since
    FROM active_sessions s CROSS JOIN clock
    LEFT JOIN active_heads h ON h.video_id=s.video_id
    LEFT JOIN active_availability availability ON availability.video_id=s.video_id
    WHERE s.status='UPCOMING'
      AND (s.scheduled_start_time<clock.as_of OR (s.lifecycle_origin='legacy_unknown' AND s.scheduled_start_time IS NULL))
      AND NOT EXISTS (SELECT 1 FROM reviewed_videos reviewed WHERE reviewed.video_id=s.video_id)
)
SELECT video_id,channel_id,is_upcoming,clock.as_of,
       positive_at,positive_seen_at,availability_at,availability_seen_at,unresolvable_since
FROM members CROSS JOIN clock
ORDER BY video_id COLLATE "C"
LIMIT $2
