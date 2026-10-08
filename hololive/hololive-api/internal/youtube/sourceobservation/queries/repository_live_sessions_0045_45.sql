SELECT video_id, channel_id, status, COALESCE(title, '') AS title, topic_id, thumbnail_url,
       scheduled_start_time, started_at, ended_at, live_first_seen_at, last_seen_at,
       is_premiere, lifecycle_origin, status_observed_at, schedule_observed_at, title_observed_at
FROM youtube_live_sessions
-- payload 영상은 상태와 관계없이 잠급니다(KEEP_ENDED 판정과 잠금 순서에 필요).
-- 채널 범위의 ENDED는 되돌아가지 않고 부재·저장 종료도 적용되지 않습니다. 남는 효과는 due
-- candidate 정리뿐이므로 candidate가 남은 ENDED만 함께 잠급니다. 잠금 대상은 session 행으로
-- 한정해 session → head → pending 잠금 순서를 유지합니다.
WHERE video_id = ANY($2::text[])
   OR (channel_id = ANY($1::text[])
       AND (status IN ('UPCOMING', 'LIVE')
            OR video_id IN (SELECT head.video_id
                            FROM youtube_live_reconciliation_heads AS head
                            WHERE head.next_end_check_at IS NOT NULL)))
ORDER BY video_id
FOR UPDATE OF youtube_live_sessions
