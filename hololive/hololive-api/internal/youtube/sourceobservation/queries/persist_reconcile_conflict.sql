INSERT INTO source_reconciliation_conflicts (
    observation_id,
    provider,
    observation_kind,
    subject_key,
    observation_key,
    evidence_sha256,
    entity_kind,
    entity_key,
    field_name,
    effective_at,
    existing_value_sha256,
    attempted_value_sha256,
    decision
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (observation_id, entity_kind, entity_key, field_name) DO NOTHING
