UPDATE youtube_collection_projection_generations
SET valid_until = $2
WHERE generation = $1 AND status = 'CURRENT'
