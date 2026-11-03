INSERT INTO source_observation_applications (
    observation_id,
    entity_kind_id,
    entity_key,
    decision_id
)
SELECT $1,
       source_observation_entity_kind_id(application.entity_kind),
       application.entity_key,
       source_observation_decision_id(application.decision)
FROM unnest($2::text[], $3::text[], $4::text[]) WITH ORDINALITY
    AS application(entity_kind, entity_key, decision, ordinal)
ORDER BY application.ordinal
ON CONFLICT (observation_id, entity_kind_id, entity_key) DO NOTHING
