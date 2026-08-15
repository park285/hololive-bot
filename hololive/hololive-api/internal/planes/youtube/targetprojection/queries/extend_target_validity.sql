UPDATE youtube_collection_targets
SET valid_until = $2
WHERE projection_generation = $1
