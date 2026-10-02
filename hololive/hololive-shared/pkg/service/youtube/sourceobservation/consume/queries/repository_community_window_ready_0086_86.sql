SELECT EXISTS (
    SELECT 1
    FROM source_observation_applications
    WHERE observation_id = $1
      AND entity_kind = $2
      AND entity_key = $3
      AND decision = $4
)
