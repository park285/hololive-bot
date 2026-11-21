SELECT observation_kind, current_generation
FROM observation_contract_generations
WHERE provider = $1
  AND observation_kind = ANY($2::text[])
