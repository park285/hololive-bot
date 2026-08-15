SELECT scheduled_for,
       observation_id,
       evidence_sha256,
       effective_at,
       received_at,
       scope_sha256,
       coverage
FROM youtube_content_absence_slots
WHERE channel_id = $1
  AND observation_kind = $2
FOR UPDATE
