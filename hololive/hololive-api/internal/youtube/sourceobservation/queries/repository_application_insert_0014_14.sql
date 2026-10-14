INSERT INTO source_observation_applications (
    observation_id,
    entity_kind,
    entity_key,
    decision
)
SELECT $1, application.entity_kind, application.entity_key, application.decision
FROM unnest($2::text[], $3::text[], $4::text[]) WITH ORDINALITY
    AS application(entity_kind, entity_key, decision, ordinal)
ORDER BY application.ordinal
ON CONFLICT (observation_id, entity_kind, entity_key)
    WHERE observation_id IS NOT NULL
    DO NOTHING
