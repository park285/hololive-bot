INSERT INTO youtube_collection_projection_generations (
    status, row_count, projection_sha256, valid_until
) VALUES ('STAGING', $1, $2, $3)
RETURNING generation
