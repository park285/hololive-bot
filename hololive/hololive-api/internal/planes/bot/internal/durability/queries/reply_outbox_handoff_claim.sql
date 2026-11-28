UPDATE bot_reply_outbox
SET handoff_next_check_at = ?1 + ?3,
    row_version = row_version + 1
WHERE id IN (
    SELECT id
    FROM bot_reply_outbox
    WHERE status = 'accepted'
      AND claim_token IS NULL
      AND handoff_next_check_at <= ?1
    ORDER BY handoff_next_check_at ASC, id ASC
    LIMIT ?2
)
RETURNING id,
          iris_request_id,
          handoff_next_check_at,
          row_version,
          ?1 - accepted_at,
          attempts,
          operator_replay_grants,
          coalesce(first_attempt_at <= ?1 - ?4, 1)
