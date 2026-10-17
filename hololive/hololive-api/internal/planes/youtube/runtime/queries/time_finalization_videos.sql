-- 신규 종료와 과거 묶음은 운영 roster와 무관하다. 신규는 최근 종료부터 $2개다.
-- 과거 행은 최근 종료부터 $1개씩 묶는다. 재시도 중인 행과 활성 묶음을 합쳐 $3개를 넘지 않는다.
-- 활성 묶음은 모든 행이 첫 결과를 낼 때까지 최종 결과 행도 유지해 묶음 안에서 membership이 바뀌지 않는다.
WITH candidates AS MATERIALIZED (
    SELECT s.video_id, s.channel_id, s.status_observed_at, f.queue, f.next_attempt_at
    FROM youtube_live_sessions s
    LEFT JOIN youtube_live_reconciliation_heads h USING (video_id)
    LEFT JOIN youtube_live_time_finalizations f USING (video_id)
    WHERE s.status = 'ENDED' AND s.times_source = 'observed'
      AND COALESCE(h.end_reason, '') <> 'CANCELLED_BEFORE_LIVE'
      AND (s.started_at IS NOT NULL OR h.last_live_positive_at IS NOT NULL)
      AND ((f.video_id IS NULL AND s.status_observed_at >= statement_timestamp() - interval '48 hours')
           OR f.outcome = 'pending')
), new_videos AS (
    SELECT video_id, channel_id,
           COALESCE(next_attempt_at, status_observed_at + interval '3 minutes') AS not_before
    FROM candidates
    WHERE queue IS NULL OR queue = 'new'
    ORDER BY status_observed_at DESC, video_id
    LIMIT $2
), ranked_backlog AS MATERIALIZED (
    -- 후보에서 빠진 pending은 시도할 수 없으므로 묶음 계산에서도 빼 다음 묶음을 막지 않는다.
    SELECT f.video_id, f.outcome, f.next_attempt_at, f.last_attempt_at, f.enqueued_at, f.completed_at,
           row_number() OVER (ORDER BY f.observed_ended_at DESC NULLS LAST, f.video_id) - 1 AS position
    FROM youtube_live_time_finalizations f
    LEFT JOIN candidates c ON c.video_id = f.video_id
    WHERE f.queue = 'backlog' AND $1::int > 0 AND (f.outcome <> 'pending' OR c.video_id IS NOT NULL)
), backlog AS MATERIALIZED (
    SELECT video_id, outcome, next_attempt_at, last_attempt_at, enqueued_at, completed_at, position,
           position / NULLIF($1::int, 0) AS batch_no
    FROM ranked_backlog
), active AS (
    -- 첫 결과가 없는 행이 남은 가장 앞 묶음이다. 다른 묶음의 재시도 행과 합쳐 $3개를 넘으면 열지 않는다.
    SELECT first_open.batch_no
    FROM (
        SELECT min(batch_no) FILTER (WHERE outcome = 'pending' AND next_attempt_at IS NULL) AS batch_no
        FROM backlog
    ) first_open
    WHERE first_open.batch_no IS NOT NULL
      AND (SELECT count(retrying.video_id) FROM backlog retrying
           WHERE retrying.outcome = 'pending' AND retrying.next_attempt_at IS NOT NULL
             AND retrying.batch_no <> first_open.batch_no) + $1::int <= $3::int
), turn AS (
    -- 첫 시도는 한 번에 한 영상만 연다. 마지막 과거 행 결과(없으면 투입) 6초 뒤이고, 결과마다 다음 영상으로 넘어간다.
    SELECT b.video_id,
           GREATEST((SELECT max(last_attempt_at) FROM backlog), b.enqueued_at) + interval '6 seconds' AS not_before
    FROM backlog b
    JOIN active a ON a.batch_no = b.batch_no
    WHERE b.outcome = 'pending' AND b.next_attempt_at IS NULL
    ORDER BY b.position
    LIMIT 1
), backlog_videos AS (
    -- 차례를 기다리는 행과 최종 결과 행은 먼 시각으로 두어 membership만 유지한다.
    SELECT b.video_id, s.channel_id,
           CASE WHEN b.outcome <> 'pending' THEN b.completed_at + interval '30 days'
                WHEN b.next_attempt_at IS NOT NULL THEN b.next_attempt_at
                WHEN t.video_id IS NOT NULL THEN t.not_before
                ELSE b.enqueued_at + interval '365 days'
           END AS not_before
    FROM backlog b
    JOIN youtube_live_sessions s ON s.video_id = b.video_id
    LEFT JOIN active a ON a.batch_no = b.batch_no
    LEFT JOIN turn t ON t.video_id = b.video_id
    WHERE a.batch_no IS NOT NULL OR (b.outcome = 'pending' AND b.next_attempt_at IS NOT NULL)
)
SELECT video_id, channel_id, not_before FROM new_videos
UNION ALL
SELECT video_id, channel_id, not_before FROM backlog_videos
