SELECT id
FROM source_observations
WHERE id = $1
FOR UPDATE
