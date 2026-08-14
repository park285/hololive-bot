UPDATE source_observation_queue
SET available_at = NOW() + INTERVAL '1 hour'
WHERE observation_id = (
    SELECT MIN(id)
    FROM source_observations
    WHERE provider = 'youtubejs'
      AND observation_kind = 'community_page'
      AND subject_key = $1
)
