-- 운영 roster의 LIVE와 지난 일정·출처 미상 UPCOMING을 신선도와 무관한 구조 membership으로 읽는다.
-- 현재 snapshot과 일치하는 검토 영수증이 있는 UPCOMING은 제외한다. UNKNOWN은 종료 근거가 아니다.
-- 신선도 정책은 API에서 계산한다. 같은 statement의 DB 시각과 필요한 사실 시각만 반환한다.
-- 구조 membership 필터 뒤 상한 판정용 $2행을 읽어 미래 일정 때문에 적격 영상이 잘리지 않게 한다.
WITH clock AS MATERIALIZED (
    SELECT statement_timestamp() AS as_of
), review_videos AS MATERIALIZED (
    -- 영수증이 있는 운영 영상만 고정합니다. 영수증 없는 후보의 snapshot을
    -- 계산하거나 과거 영수증마다 같은 현재 snapshot을 다시 읽지 않습니다.
    SELECT DISTINCT receipt.video_id
    FROM youtube_live_review_receipts receipt
    JOIN youtube_live_sessions s ON s.video_id=receipt.video_id
    WHERE s.status='UPCOMING' AND s.channel_id=ANY($1::text[])
), review_snapshots AS MATERIALIZED (
    SELECT video.video_id,
           (SELECT snapshot_sha256 FROM youtube_live_review_snapshot(video.video_id)) AS snapshot_sha256
    FROM review_videos video
), members AS (
    SELECT s.video_id,s.channel_id,FALSE AS is_upcoming,
           h.last_live_positive_at AS positive_at,
           h.last_live_positive_seen_at AS positive_seen_at,
           NULL::timestamptz AS availability_at,
           NULL::timestamptz AS availability_seen_at
    FROM youtube_live_sessions s CROSS JOIN clock
    LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id=s.video_id
    WHERE s.status='LIVE' AND s.channel_id=ANY($1::text[])
    UNION ALL
    SELECT s.video_id,s.channel_id,TRUE AS is_upcoming,
           h.last_upcoming_positive_at AS positive_at,
           h.last_upcoming_positive_seen_at AS positive_seen_at,
           availability.effective_at AS availability_at,
           availability.observed_at AS availability_seen_at
    FROM youtube_live_sessions s CROSS JOIN clock
    LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id=s.video_id
    LEFT JOIN youtube_video_availability availability ON availability.video_id=s.video_id
    WHERE s.status='UPCOMING' AND s.channel_id=ANY($1::text[])
      AND (s.scheduled_start_time<clock.as_of OR (s.lifecycle_origin='legacy_unknown' AND s.scheduled_start_time IS NULL))
      AND NOT EXISTS (
          SELECT 1 FROM review_snapshots review
          JOIN youtube_live_review_receipts receipt
            ON receipt.video_id=review.video_id AND receipt.snapshot_sha256=review.snapshot_sha256
          WHERE review.video_id=s.video_id)
)
SELECT video_id,channel_id,is_upcoming,clock.as_of,
       positive_at,positive_seen_at,availability_at,availability_seen_at
FROM members CROSS JOIN clock
ORDER BY video_id COLLATE "C"
LIMIT $2
