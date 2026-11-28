coalesce(
    candidate.attempts >= ?2 + candidate.operator_replay_grants
    OR candidate.first_attempt_at <= ?1 - ?3,
    0
)
