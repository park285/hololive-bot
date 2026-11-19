SELECT scheduled_for,
       observation_id,
       encode(evidence_sha256, 'hex') AS evidence_sha256,
       effective_at,
       received_at,
       encode(scope_sha256, 'hex') AS scope_sha256,
       coverage
FROM youtube_content_absence_slots
WHERE channel_id = $1
  AND observation_kind = $2
  AND (scheduled_for = $3 OR effective_at > $4)
ORDER BY scheduled_for
FOR UPDATE
