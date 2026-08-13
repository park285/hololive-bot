SELECT observed_at
FROM source_observation_outbox
WHERE id = $1
  AND source_kind = $2
  AND status = 'PROCESSING'
  AND lease_token = $3
  AND generation = $4
FOR UPDATE
