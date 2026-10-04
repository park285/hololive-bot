-- 운영 roster의 LIVE와 지난 일정·출처 미상 UPCOMING을 신선도와 무관한 구조 membership으로 읽는다.
-- 현재 원본에 적용되는 검토 영수증(youtube_live_review_current_receipt)이 있는 UPCOMING은 제외한다. UNKNOWN은 종료 근거가 아니다.
-- 신선도는 not_before로만 내보낸다. 한 근거의 두 시각이 모두 조회 statement 시각 이하일 때만 받아들이고,
-- 그 중 이른 시각에 예산을 더한다. 미래 시각·NULL 시각은 근거가 아니며, 지난 not_before는 즉시 확인 가능이다.
-- LIVE는 LIVE positive만, UPCOMING은 UPCOMING positive와 가용성 확인(UNKNOWN 포함)을 기존 정책대로 쓴다.
-- 상한 판정용으로 $3행까지만 고정 순서로 읽는다. 호출자는 초과를 거부하고 절단 결과를 쓰지 않는다.
WITH clock AS MATERIALIZED (
    SELECT statement_timestamp() AS as_of,
           $2::bigint * INTERVAL '1 millisecond' AS budget
), review_videos AS MATERIALIZED (
    -- 영수증이 있는 운영 영상만 고정합니다. 영수증 없는 후보의 snapshot을
    -- 계산하거나 과거 영수증마다 같은 현재 snapshot을 다시 읽지 않습니다.
    SELECT DISTINCT receipt.video_id
    FROM youtube_live_review_receipts receipt
    JOIN youtube_live_sessions s ON s.video_id=receipt.video_id
    WHERE s.status='UPCOMING' AND s.channel_id=ANY($1::text[])
), reviewed_videos AS MATERIALIZED (
    -- 영상마다 현재 원본 판정을 한 번만 계산합니다. 의미 사실이 바뀐 영상은 다시 확인 대상입니다.
    SELECT video.video_id
    FROM review_videos video
    WHERE (SELECT review.reviewed_at FROM youtube_live_review_current_receipt(video.video_id) review) IS NOT NULL
), members AS (
    SELECT s.video_id,s.channel_id,FALSE AS is_upcoming,
           CASE WHEN h.last_live_positive_at<=clock.as_of AND h.last_live_positive_seen_at<=clock.as_of
                THEN LEAST(h.last_live_positive_at,h.last_live_positive_seen_at)+clock.budget
           END AS not_before
    FROM youtube_live_sessions s CROSS JOIN clock
    LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id=s.video_id
    WHERE s.status='LIVE' AND s.channel_id=ANY($1::text[])
    UNION ALL
    SELECT s.video_id,s.channel_id,TRUE AS is_upcoming,
           -- GREATEST는 NULL을 무시하므로 받아들인 근거 중 가장 늦은 다음 확인 시각이 된다.
           GREATEST(
               CASE WHEN h.last_upcoming_positive_at<=clock.as_of AND h.last_upcoming_positive_seen_at<=clock.as_of
                    THEN LEAST(h.last_upcoming_positive_at,h.last_upcoming_positive_seen_at)+clock.budget
               END,
               CASE WHEN availability.effective_at<=clock.as_of AND availability.observed_at<=clock.as_of
                    THEN LEAST(availability.effective_at,availability.observed_at)+clock.budget
               END) AS not_before
    FROM youtube_live_sessions s CROSS JOIN clock
    LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id=s.video_id
    LEFT JOIN youtube_video_availability availability ON availability.video_id=s.video_id
    WHERE s.status='UPCOMING' AND s.channel_id=ANY($1::text[])
      AND (s.scheduled_start_time<clock.as_of OR (s.lifecycle_origin='legacy_unknown' AND s.scheduled_start_time IS NULL))
      AND NOT EXISTS (SELECT 1 FROM reviewed_videos reviewed WHERE reviewed.video_id=s.video_id)
)
SELECT video_id,channel_id,is_upcoming,not_before
FROM members
ORDER BY video_id COLLATE "C"
LIMIT $3
