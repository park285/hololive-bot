SELECT generation, row_count, projection_sha256,
       (pg_current_xact_id()::text::bigint >> 32)::text || ':' || xmin::text
FROM youtube_collection_projection_generations
WHERE status = 'CURRENT'
FOR UPDATE
