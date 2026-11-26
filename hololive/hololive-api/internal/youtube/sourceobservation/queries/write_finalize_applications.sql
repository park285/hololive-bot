WITH application AS MATERIALIZED (
    SELECT source_observation_entity_kind_id(input.entity_kind) AS entity_kind_id,
           input.entity_kind,
           input.entity_key,
           source_observation_decision_id(input.decision) AS decision_id,
           input.ordinal
    FROM unnest($2::text[], $3::text[], $4::text[]) WITH ORDINALITY
        AS input(entity_kind, entity_key, decision, ordinal)
), live_session_head AS (
    INSERT INTO youtube_live_session_application_heads (video_id, observation_id)
    SELECT DISTINCT application.entity_key, $1::bigint
    FROM application
    WHERE application.entity_kind = 'youtube_live_session'
    ORDER BY application.entity_key
    ON CONFLICT (video_id) DO UPDATE
    SET observation_id = EXCLUDED.observation_id
    WHERE youtube_live_session_application_heads.observation_id < EXCLUDED.observation_id
)
INSERT INTO source_observation_applications (
    observation_id,
    entity_kind_id,
    entity_key,
    decision_id
)
SELECT $1, application.entity_kind_id, application.entity_key, application.decision_id
FROM application
ORDER BY application.ordinal
ON CONFLICT (observation_id, entity_kind_id, entity_key) DO NOTHING
