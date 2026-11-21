UPDATE youtube_collection_targets
SET member_since_generation = NULL
WHERE projection_generation = $1
