SELECT EXISTS (
    SELECT 1
    FROM source_observation_applications
    WHERE observation_id = $1
      AND entity_kind_id = (SELECT kind.id FROM source_observation_entity_kinds AS kind WHERE kind.name = $2)
      AND entity_key = $3
      AND decision_id = (SELECT decision.id FROM source_observation_decisions AS decision WHERE decision.name = $4)
)
