UPDATE youtube_collection_projection_generations
SET status = 'RETIRED'
WHERE generation = $1 AND status = 'CURRENT'
