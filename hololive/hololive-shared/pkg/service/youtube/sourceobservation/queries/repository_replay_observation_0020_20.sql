SELECT provider,
       observation_kind,
       subject_key,
       observation_key,
       schema_version,
       contract_generation,
       evidence_sha256
FROM source_observations
WHERE id = $1
FOR SHARE
