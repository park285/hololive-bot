SELECT current_schema_version,
       current_generation
FROM observation_contract_generations
WHERE provider = $1
  AND observation_kind = $2
FOR SHARE
