WITH changed AS (
    UPDATE youtube_collection_targets
    SET not_before = clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond')
    WHERE projection_generation = $1
    RETURNING projection_generation
)
UPDATE youtube_collection_projection_generations
SET eligibility_version = eligibility_version + 1
WHERE generation = $1 AND EXISTS (SELECT 1 FROM changed)
