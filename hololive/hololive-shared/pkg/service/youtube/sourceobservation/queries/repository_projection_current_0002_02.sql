SELECT generation
FROM youtube_collection_projection_generations
WHERE generation = $1
  AND status = 'CURRENT'
  AND valid_until > clock_timestamp()
FOR SHARE
