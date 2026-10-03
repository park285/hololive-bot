INSERT INTO source_observation_applications (
    observation_id,
    provider,
    observation_kind,
    subject_key,
    evidence_sha256,
    entity_kind,
    entity_key,
    decision,
    effective_at
)
SELECT $1, $2, $3, $4, $5, application.entity_kind, application.entity_key, application.decision, $9
FROM unnest($6::text[], $7::text[], $8::text[]) WITH ORDINALITY
    AS application(entity_kind, entity_key, decision, ordinal)
ORDER BY application.ordinal
ON CONFLICT (observation_id, entity_kind, entity_key)
    WHERE observation_id IS NOT NULL
    DO NOTHING
