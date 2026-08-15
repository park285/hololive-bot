INSERT INTO youtube_collection_target_reasons (
    projection_generation, subject_key, observation_kind, reason_kind, reason_key
)
SELECT $1, input.subject_key, input.observation_kind, input.reason_kind, input.reason_key
FROM unnest($2::text[], $3::text[], $4::text[], $5::text[])
     AS input(subject_key, observation_kind, reason_kind, reason_key)
