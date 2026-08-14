SELECT count(*)
FROM source_observations
WHERE provider = 'youtubejs'
  AND observation_kind = 'community_page'
  AND subject_key = $1
