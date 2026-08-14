INSERT INTO youtube_collection_projection_generations (
    status,
    row_count,
    projection_sha256,
    valid_until,
    activated_at
) VALUES ('CURRENT', 1, $1, NOW() + INTERVAL '1 day', NOW())
RETURNING generation
