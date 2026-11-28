INSERT INTO bot_reply_outbox (
    id, message_id, phase, ordinal, room_id, payload, payload_hash, client_request_id, status, attempts,
    first_attempt_at, iris_request_id, claim_token, lease_until, last_error, created_at, updated_at,
    available_at, operator_replay_grants, accepted_at, handoff_next_check_at
) VALUES (
    ?1, ?2, ?3, ?4, ?5,
    CASE WHEN ?9 = 'discarded' THEN '{}' ELSE ?6 END,
    ?7, ?8,
    CASE WHEN ?9 = 'discarded' THEN 'manual_review' ELSE ?9 END,
    ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18, ?19, ?20, ?21
)
