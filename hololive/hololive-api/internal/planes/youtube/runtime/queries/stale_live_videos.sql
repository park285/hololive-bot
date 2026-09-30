-- LIVE의 현행 초과 거부와 우선순위를 유지하고 남은 상한 안에서 지난 일정·출처
-- 미상을 확인한다. 최근 UNKNOWN도 시도 신선도로 사용하되 positive로 해석하지 않는다.
WITH clock AS MATERIALIZED (
    SELECT statement_timestamp() AS as_of
), live_candidates AS MATERIALIZED (
    SELECT s.video_id,s.channel_id,0 AS priority,NULL::timestamptz AS checked_at
    FROM youtube_live_sessions s CROSS JOIN clock
    LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id=s.video_id
    WHERE s.status='LIVE' AND s.channel_id=ANY($1::text[])
      AND NOT COALESCE(
          h.last_live_positive_at BETWEEN clock.as_of-$2::bigint*INTERVAL '1 millisecond' AND clock.as_of
          AND h.last_live_positive_seen_at BETWEEN clock.as_of-$2::bigint*INTERVAL '1 millisecond' AND clock.as_of,false)
    ORDER BY s.video_id COLLATE "C" LIMIT $3
), upcoming_candidates AS MATERIALIZED (
    SELECT s.video_id,s.channel_id,1 AS priority,availability.observed_at AS checked_at
    FROM youtube_live_sessions s CROSS JOIN clock
    LEFT JOIN youtube_live_reconciliation_heads h ON h.video_id=s.video_id
    LEFT JOIN youtube_video_availability availability ON availability.video_id=s.video_id
    WHERE s.status='UPCOMING' AND s.channel_id=ANY($1::text[])
      AND (s.scheduled_start_time<clock.as_of OR (s.lifecycle_origin='legacy_unknown' AND s.scheduled_start_time IS NULL))
      AND NOT COALESCE(
          h.last_upcoming_positive_at BETWEEN clock.as_of-$2::bigint*INTERVAL '1 millisecond' AND clock.as_of
          AND h.last_upcoming_positive_seen_at BETWEEN clock.as_of-$2::bigint*INTERVAL '1 millisecond' AND clock.as_of,false)
      AND NOT COALESCE(
          availability.effective_at BETWEEN clock.as_of-$2::bigint*INTERVAL '1 millisecond' AND clock.as_of
          AND availability.observed_at BETWEEN clock.as_of-$2::bigint*INTERVAL '1 millisecond' AND clock.as_of,false)
      AND NOT EXISTS (
          SELECT 1 FROM youtube_live_review_receipts receipt
          WHERE receipt.video_id=s.video_id
            AND receipt.snapshot_sha256=(SELECT snapshot_sha256 FROM youtube_live_review_snapshot(s.video_id)))
    ORDER BY availability.observed_at NULLS FIRST,s.video_id COLLATE "C"
    LIMIT GREATEST($3-1-(SELECT count(video_id) FROM live_candidates),0)
)
SELECT video_id,channel_id,priority=1 AS is_upcoming FROM (
    SELECT video_id,channel_id,priority,checked_at FROM live_candidates
    UNION ALL SELECT video_id,channel_id,priority,checked_at FROM upcoming_candidates
) candidates ORDER BY priority,checked_at NULLS FIRST,video_id COLLATE "C"
