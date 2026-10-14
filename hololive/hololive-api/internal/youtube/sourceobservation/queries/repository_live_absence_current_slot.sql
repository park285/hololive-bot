SELECT observation_id, scheduled_for, encode(evidence_sha256, 'hex'), effective_at, received_at, encode(scope_sha256, 'hex'), coverage
FROM youtube_live_absence_slots
WHERE scheduled_for = $1 AND (coverage -> 'requested_channel_ids') ?| $2::text[]
ORDER BY scheduled_for, observation_id
