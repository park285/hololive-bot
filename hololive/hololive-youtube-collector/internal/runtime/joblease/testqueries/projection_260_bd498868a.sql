SELECT generation
FROM youtube_collection_projection_generations
WHERE status = 'CURRENT'
  AND valid_until > clock_timestamp()
