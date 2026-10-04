INSERT INTO youtube_collection_projection_generations (
    status, row_count, projection_sha256, valid_until, validity_refreshed_at
) VALUES ('STAGING', $1, $2, $3, $4)
RETURNING generation
