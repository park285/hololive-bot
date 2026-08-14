SELECT provider,
       observation_kind,
       subject_key,
       observation_key,
       schema_version,
       contract_generation,
       evidence_sha256
FROM lock_source_observation($1)
