COALESCE(
    candidate.attempts >= {{max_attempts}} + candidate.operator_replay_grants
    OR candidate.first_attempt_at <= statement_timestamp() - ({{replay_horizon}}::bigint * INTERVAL '1 millisecond'),
    false
)
