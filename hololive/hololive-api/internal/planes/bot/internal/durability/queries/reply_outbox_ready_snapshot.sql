WITH ready AS (
    SELECT candidate.created_at
    FROM bot_reply_outbox AS candidate
    WHERE candidate.status IN ('pending', 'retryable_pre_dispatch', 'outcome_unknown')
      AND candidate.available_at <= clock_timestamp()
      AND candidate.attempts < $1 + candidate.operator_replay_grants
      AND (
          candidate.first_attempt_at IS NULL
          OR candidate.first_attempt_at > clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond')
      )
      AND NOT EXISTS (
          SELECT 1
          FROM bot_reply_outbox AS predecessor
          WHERE predecessor.room_id = candidate.room_id
            AND predecessor.id < candidate.id
            AND predecessor.status IN (
                'pending', 'submitting', 'accepted', 'retryable_pre_dispatch', 'outcome_unknown'
            )
      )
)
SELECT COUNT(*),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(created_at))), 0), 0)
FROM ready
