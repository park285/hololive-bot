WITH input AS MATERIALIZED (
    SELECT ordinal,
           identity,
           provider,
           observation_kind,
           subject_key,
           observation_key,
           schema_version,
           contract_generation,
           scheduled_for,
           observed_at,
           source_event_at,
           scope_sha256,
           completeness,
           continuity,
           payload,
           payload_sha256,
           evidence_sha256,
           collector_instance,
           job_key,
           collection_job_kind,
           fence_epoch,
           projection_generation,
           collection_latency_ms,
           cursor
    FROM jsonb_to_recordset($1::jsonb) AS value(
        ordinal integer,
        identity text,
        provider text,
        observation_kind text,
        subject_key text,
        observation_key text,
        schema_version smallint,
        contract_generation bigint,
        scheduled_for timestamptz,
        observed_at timestamptz,
        source_event_at timestamptz,
        scope_sha256 text,
        completeness text,
        continuity text,
        payload jsonb,
        payload_sha256 text,
        evidence_sha256 text,
        collector_instance text,
        job_key text,
        collection_job_kind text,
        fence_epoch bigint,
        projection_generation bigint,
        collection_latency_ms bigint,
        cursor jsonb
    )
), identity_locks AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(hashtextextended(identity, 0)) AS acquired
    FROM input
    ORDER BY identity
), lock_barrier AS MATERIALIZED (
    SELECT count(acquired) AS acquired_count
    FROM identity_locks
), existing AS MATERIALIZED (
    SELECT input.ordinal,
           input.identity,
           input.provider,
           input.observation_kind,
           input.subject_key,
           input.observation_key,
           input.schema_version,
           input.contract_generation,
           input.scheduled_for,
           input.observed_at,
           input.source_event_at,
           input.scope_sha256,
           input.completeness,
           input.continuity,
           input.payload,
           input.payload_sha256,
           input.evidence_sha256,
           input.collector_instance,
           input.job_key,
           input.collection_job_kind,
           input.fence_epoch,
           input.projection_generation,
           input.collection_latency_ms,
           input.cursor,
           current.id AS existing_id,
           current.evidence_sha256 AS existing_evidence_sha256
    FROM input
    CROSS JOIN lock_barrier
    LEFT JOIN LATERAL lock_source_observation_identity(
        input.provider,
        input.observation_kind,
        input.subject_key,
        input.observation_key,
        input.schema_version,
        input.contract_generation
    ) AS current ON TRUE
), collision_state AS MATERIALIZED (
    SELECT COALESCE(
        bool_or(
            existing_id IS NOT NULL
            AND existing_evidence_sha256 <> evidence_sha256
        ),
        FALSE
    ) AS has_collision
    FROM existing
), collision_write AS (
    INSERT INTO source_observation_collisions (
        existing_observation_id,
        provider,
        observation_kind,
        subject_key,
        observation_key,
        schema_version,
        contract_generation,
        existing_evidence_sha256,
        attempted_evidence_sha256,
        attempted_payload_sha256,
        collector_instance,
        job_key,
        fence_epoch
    )
    SELECT existing.existing_id,
           existing.provider,
           existing.observation_kind,
           existing.subject_key,
           existing.observation_key,
           existing.schema_version,
           existing.contract_generation,
           existing.existing_evidence_sha256,
           existing.evidence_sha256,
           existing.payload_sha256,
           existing.collector_instance,
           existing.job_key,
           existing.fence_epoch
    FROM existing
    CROSS JOIN collision_state
    WHERE collision_state.has_collision
      AND existing.existing_id IS NOT NULL
      AND existing.existing_evidence_sha256 <> existing.evidence_sha256
    RETURNING 1 AS inserted
), observation_write AS (
    INSERT INTO source_observations (
        provider,
        observation_kind,
        subject_key,
        observation_key,
        schema_version,
        contract_generation,
        scheduled_for,
        observed_at,
        source_event_at,
        scope_sha256,
        completeness,
        continuity,
        payload,
        payload_sha256,
        evidence_sha256,
        collector_instance,
        job_key,
        collection_job_kind,
        fence_epoch,
        projection_generation
    )
    SELECT existing.provider,
           existing.observation_kind,
           existing.subject_key,
           existing.observation_key,
           existing.schema_version,
           existing.contract_generation,
           existing.scheduled_for,
           existing.observed_at,
           existing.source_event_at,
           existing.scope_sha256,
           existing.completeness,
           existing.continuity,
           existing.payload,
           existing.payload_sha256,
           existing.evidence_sha256,
           existing.collector_instance,
           existing.job_key,
           existing.collection_job_kind,
           existing.fence_epoch,
           existing.projection_generation
    FROM existing
    CROSS JOIN collision_state
    WHERE NOT collision_state.has_collision
      AND existing.existing_id IS NULL
    ORDER BY existing.ordinal
    RETURNING id,
              provider,
              observation_kind,
              subject_key,
              observation_key,
              schema_version,
              contract_generation
), queue_write AS (
    INSERT INTO source_observation_queue (observation_id)
    SELECT id
    FROM observation_write
    ON CONFLICT (observation_id) DO NOTHING
    RETURNING observation_id
), checkpoint_write AS (
    INSERT INTO source_collection_checkpoints (
        provider,
        observation_kind,
        subject_key,
        scope_sha256,
        contract_generation,
        last_observation_key,
        last_evidence_sha256,
        last_scheduled_for,
        last_success_at,
        collection_latency_ms,
        continuity,
        cursor,
        last_error_code,
        last_error_at
    )
    SELECT existing.provider,
           existing.observation_kind,
           existing.subject_key,
           existing.scope_sha256,
           existing.contract_generation,
           existing.observation_key,
           existing.evidence_sha256,
           existing.scheduled_for,
           NOW(),
           existing.collection_latency_ms,
           existing.continuity,
           existing.cursor,
           NULL,
           NULL
    FROM existing
    CROSS JOIN collision_state
    WHERE NOT collision_state.has_collision
    ON CONFLICT (provider, observation_kind, subject_key, scope_sha256) DO UPDATE
    SET contract_generation = EXCLUDED.contract_generation,
        last_observation_key = EXCLUDED.last_observation_key,
        last_evidence_sha256 = EXCLUDED.last_evidence_sha256,
        last_scheduled_for = EXCLUDED.last_scheduled_for,
        last_success_at = EXCLUDED.last_success_at,
        collection_latency_ms = EXCLUDED.collection_latency_ms,
        continuity = EXCLUDED.continuity,
        cursor = EXCLUDED.cursor,
        last_error_code = NULL,
        last_error_at = NULL,
        updated_at = NOW()
    RETURNING provider
), effects AS MATERIALIZED (
    SELECT (SELECT count(inserted) FROM collision_write)
         + (SELECT count(observation_id) FROM queue_write)
         + (SELECT count(provider) FROM checkpoint_write) AS affected_count
)
SELECT existing.ordinal,
       COALESCE(existing.existing_id, observation_write.id, 0) AS observation_id,
       CASE
           WHEN collision_state.has_collision THEN 'COLLISION'
           WHEN existing.existing_id IS NOT NULL THEN 'DUPLICATE'
           ELSE 'INSERTED'
       END AS outcome
FROM existing
CROSS JOIN collision_state
CROSS JOIN effects
LEFT JOIN observation_write
  ON observation_write.provider = existing.provider
 AND observation_write.observation_kind = existing.observation_kind
 AND observation_write.subject_key = existing.subject_key
 AND observation_write.observation_key = existing.observation_key
 AND observation_write.schema_version = existing.schema_version
 AND observation_write.contract_generation = existing.contract_generation
ORDER BY existing.ordinal
