SELECT observation_id, scheduled_for, encode(evidence_sha256, 'hex'), effective_at, received_at, encode(scope_sha256, 'hex'), coverage
FROM youtube_live_absence_slots
WHERE ((coverage -> 'requested_channel_ids') ?| $1::text[]
       AND ($2::timestamptz IS NULL OR effective_at > $2))
   OR (scheduled_for = $3 AND (coverage -> 'requested_channel_ids') ?| $4::text[])
ORDER BY scheduled_for, observation_id
