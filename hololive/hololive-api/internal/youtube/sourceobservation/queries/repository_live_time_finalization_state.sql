-- 세션 행은 loadLiveState가 이미 잠갔다. 확정 진행 행은 session → head → pending 다음 순서로 잠근다.
SELECT session.times_source,
       progress.queue, progress.outcome, progress.attempts, progress.deferrals,
       progress.retry_base_at, progress.next_attempt_at, progress.observation_id, progress.enqueued_at
FROM youtube_live_sessions AS session
LEFT JOIN LATERAL (
    SELECT finalization.queue, finalization.outcome, finalization.attempts, finalization.deferrals,
           finalization.retry_base_at, finalization.next_attempt_at, finalization.observation_id,
           finalization.enqueued_at
    FROM youtube_live_time_finalizations AS finalization
    WHERE finalization.video_id = session.video_id
    FOR UPDATE
) AS progress ON TRUE
WHERE session.video_id = $1
