FROM bot_reply_outbox AS candidate
WHERE candidate.status IN ('pending', 'retryable_pre_dispatch', 'outcome_unknown')
  AND candidate.available_at <= ?1
  AND NOT EXISTS (
      SELECT 1
      FROM bot_reply_outbox AS predecessor
      WHERE predecessor.room_id = candidate.room_id
        AND predecessor.id < candidate.id
        AND predecessor.status IN (
            'pending', 'submitting', 'accepted', 'retryable_pre_dispatch', 'outcome_unknown'
        )
  )
