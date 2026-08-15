UPDATE youtube_collection_projection_generations
SET status = 'CURRENT', activated_at = $2
WHERE generation = $1 AND status = 'STAGING'
