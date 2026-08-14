DELETE FROM source_observations
WHERE id = (
    SELECT source_observation_id
    FROM source_observation_subject_heads
    WHERE provider = 'youtubejs'
      AND observation_kind = 'community_page'
      AND subject_key = $1
)
