SELECT id,
       evidence_sha256
FROM lock_source_observation_identity($1, $2, $3, $4, $5, $6)
