SELECT observation.id
FROM source_observations AS observation
WHERE observation.observation_kind = $1
  AND observation.received_at < $2
  AND NOT public.source_observation_retention_protected(observation.id)
ORDER BY observation.received_at, observation.id
LIMIT $3
