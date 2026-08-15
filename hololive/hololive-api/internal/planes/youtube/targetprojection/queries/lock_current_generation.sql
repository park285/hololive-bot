SELECT generation, row_count, projection_sha256
FROM youtube_collection_projection_generations
WHERE status = 'CURRENT'
FOR UPDATE
