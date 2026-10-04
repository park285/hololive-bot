SELECT checkpoint.cursor
FROM source_collection_checkpoints AS checkpoint
JOIN observation_contract_generations AS current_contract
  ON current_contract.provider = checkpoint.provider
 AND current_contract.observation_kind = checkpoint.observation_kind
 AND current_contract.current_generation = checkpoint.contract_generation
WHERE checkpoint.provider = $1
  AND checkpoint.observation_kind = $2
  AND checkpoint.subject_key = $3
ORDER BY checkpoint.updated_at DESC, checkpoint.scope_sha256
LIMIT 1
