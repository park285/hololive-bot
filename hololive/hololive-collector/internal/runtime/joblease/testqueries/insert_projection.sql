INSERT INTO youtube_collection_projection_generations (
    status,
    row_count,
    projection_sha256,
    valid_until,
    activated_at
) VALUES ('CURRENT', $1, repeat('a', 64), clock_timestamp() + INTERVAL '1 hour', clock_timestamp())
RETURNING generation
