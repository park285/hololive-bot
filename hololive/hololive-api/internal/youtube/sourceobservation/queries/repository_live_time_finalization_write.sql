-- 확정 진행 행과, 확정이면 세션·head 시각을 한 statement로 쓴다.
-- 진행 행은 잠근 뒤 읽은 상태($15~$17)일 때만 갱신한다. 다르면 행을 돌려주지 않아 호출자가 트랜잭션을 실패시킨다.
-- 새 행의 observed_* 사본은 확정 전 세션 시각이고, 기존 행의 사본과 queue는 바꾸지 않는다.
WITH progress AS (
    INSERT INTO youtube_live_time_finalizations AS finalization (
        video_id, queue, outcome, reason, attempts, deferrals, retry_base_at, next_attempt_at,
        last_attempt_at, observation_id, observed_started_at, observed_ended_at, completed_at
    ) VALUES (
        $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        CASE WHEN $3 = 'pending' THEN NULL ELSE statement_timestamp() END
    )
    ON CONFLICT (video_id) DO UPDATE SET
        outcome = excluded.outcome,
        reason = excluded.reason,
        attempts = excluded.attempts,
        deferrals = excluded.deferrals,
        retry_base_at = excluded.retry_base_at,
        next_attempt_at = excluded.next_attempt_at,
        last_attempt_at = excluded.last_attempt_at,
        observation_id = excluded.observation_id,
        completed_at = excluded.completed_at,
        updated_at = statement_timestamp()
    WHERE finalization.outcome = 'pending'
      AND finalization.attempts = $15
      AND finalization.deferrals = $16
      AND finalization.observation_id IS NOT DISTINCT FROM $17
    RETURNING finalization.video_id, finalization.outcome
), finalized_session AS (
    UPDATE youtube_live_sessions AS session
    SET started_at = $13, ended_at = $14, times_source = 'provider'
    FROM progress
    WHERE progress.outcome = 'finalized'
      AND session.video_id = progress.video_id
      AND session.status = 'ENDED'
      AND session.times_source = 'observed'
    RETURNING session.video_id
), finalized_head AS (
    UPDATE youtube_live_reconciliation_heads AS head
    SET ended_at = $14, updated_at = statement_timestamp()
    FROM finalized_session
    WHERE head.video_id = finalized_session.video_id
      AND head.ended_at IS DISTINCT FROM $14
    RETURNING head.video_id
)
SELECT progress.outcome, EXISTS (SELECT 1 FROM finalized_session)
FROM progress
