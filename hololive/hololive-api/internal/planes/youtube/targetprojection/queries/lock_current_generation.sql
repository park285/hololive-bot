-- epoch까지 비교하여 32비트 xmin 재사용도 캐시 적중으로 오인하지 않는다.
SELECT generation, row_count, projection_sha256,
       (pg_current_xact_id()::text::bigint >> 32)::text || ':' || xmin::text
FROM youtube_collection_projection_generations
WHERE status = 'CURRENT'
FOR UPDATE
