SELECT count(*)
FROM source_observation_applications
WHERE subject_key = $1
  AND entity_kind = 'community_subject_head'
  AND decision = 'STALE_SKIPPED'
