UPDATE youtube_collection_projection_generations
SET status = 'CURRENT', activated_at = $2
WHERE generation = $1 AND status = 'STAGING'
RETURNING (pg_current_xact_id()::text::bigint >> 32)::text || ':' || xmin::text
