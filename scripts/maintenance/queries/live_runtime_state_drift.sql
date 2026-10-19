-- 집계표를 읽는 소비자와 독립적으로 원본 및 현재 일치 영수증에서 모든 scalar를 재계산한다.
WITH expected AS (
    SELECT coalesce(session.video_id, head.video_id)::text AS video_id, session.channel_id::text,
           session.status AS session_status, head.status AS head_status, session.scheduled_start_time, session.lifecycle_origin,
           head.last_live_positive_at, head.last_live_positive_seen_at,
           head.last_upcoming_positive_at, head.last_upcoming_positive_seen_at,
           availability.effective_at AS availability_at, availability.observed_at AS availability_seen_at,
           head.unresolvable_since, review.reviewed_at
    FROM public.youtube_live_sessions session
    FULL JOIN public.youtube_live_reconciliation_heads head USING (video_id)
    LEFT JOIN public.youtube_video_availability availability ON availability.video_id = coalesce(session.video_id, head.video_id)
    CROSS JOIN LATERAL public.youtube_live_review_current_receipt(coalesce(session.video_id, head.video_id)) review
    WHERE session.status IN ('LIVE', 'UPCOMING') OR head.status IN ('LIVE', 'UPCOMING')
)
SELECT count(expected.video_id) FILTER (WHERE actual.video_id IS NULL) AS missing_rows,
       count(actual.video_id) FILTER (WHERE expected.video_id IS NULL) AS extra_rows,
       count(expected.video_id) FILTER (
           WHERE actual.video_id IS NOT NULL
             AND ROW(expected.channel_id, expected.session_status, expected.head_status,
                     expected.scheduled_start_time, expected.lifecycle_origin,
                     expected.last_live_positive_at, expected.last_live_positive_seen_at,
                     expected.last_upcoming_positive_at, expected.last_upcoming_positive_seen_at,
                     expected.availability_at, expected.availability_seen_at, expected.unresolvable_since, expected.reviewed_at)
                 IS DISTINCT FROM
                 ROW(actual.channel_id, actual.session_status, actual.head_status,
                     actual.scheduled_start_time, actual.lifecycle_origin,
                     actual.last_live_positive_at, actual.last_live_positive_seen_at,
                     actual.last_upcoming_positive_at, actual.last_upcoming_positive_seen_at,
                     actual.availability_at, actual.availability_seen_at, actual.unresolvable_since, actual.reviewed_at)
       ) AS changed_rows
FROM expected FULL JOIN public.youtube_live_runtime_state actual USING (video_id);
