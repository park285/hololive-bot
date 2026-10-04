UPDATE youtube_collection_targets
SET not_before = clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond')
WHERE projection_generation = $1
