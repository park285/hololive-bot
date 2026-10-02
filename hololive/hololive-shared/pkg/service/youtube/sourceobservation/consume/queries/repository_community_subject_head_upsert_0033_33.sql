INSERT INTO source_observation_subject_heads (
    provider,
    observation_kind,
    subject_key,
    source_observation_id,
    evidence_sha256,
    effective_at
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (provider, observation_kind, subject_key) DO UPDATE
SET source_observation_id = EXCLUDED.source_observation_id,
    evidence_sha256 = EXCLUDED.evidence_sha256,
    effective_at = EXCLUDED.effective_at,
    updated_at = NOW()
WHERE source_observation_subject_heads.effective_at < EXCLUDED.effective_at
   OR (
       source_observation_subject_heads.effective_at = EXCLUDED.effective_at
       AND source_observation_subject_heads.source_observation_id < EXCLUDED.source_observation_id
   )
