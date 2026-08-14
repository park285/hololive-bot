UPDATE youtube_collection_projection_generations
SET valid_until = clock_timestamp() - INTERVAL '1 second'
WHERE generation = $1
