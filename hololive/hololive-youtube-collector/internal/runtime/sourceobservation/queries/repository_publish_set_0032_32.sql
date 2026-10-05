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
           current.evidence_sha256 AS existing_evidence_sha256,
           current.id IS NOT NULL
               AND current.evidence_sha256 <> input.evidence_sha256 AS is_collision
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
), payload_keys AS MATERIALIZED (
    -- payload는 existing에 복사하지 않고 필요한 행만 input에서 ordinal로 다시 읽는다.
    SELECT DISTINCT ON (existing.observation_kind, existing.schema_version, existing.payload_sha256)
           existing.observation_kind, existing.schema_version, existing.payload_sha256, input.payload
    FROM existing
    JOIN input ON input.ordinal = existing.ordinal
    WHERE existing.existing_id IS NULL AND NOT existing.is_collision
    ORDER BY existing.observation_kind, existing.schema_version, existing.payload_sha256, existing.ordinal
), payload_advisory_locks AS MATERIALIZED (
    SELECT pg_advisory_xact_lock(hashtextextended(
        observation_kind || ':' || schema_version::text || ':' || payload_sha256, 1
    )) AS acquired
    FROM payload_keys
    ORDER BY observation_kind, schema_version, payload_sha256
), payload_lock_barrier AS MATERIALIZED (
    SELECT count(acquired) AS acquired_count FROM payload_advisory_locks
), payload_existing AS MATERIALIZED (
    SELECT key.observation_kind, key.schema_version, key.payload_sha256,
           key.payload, stored.id
    FROM payload_keys AS key
    CROSS JOIN payload_lock_barrier
    LEFT JOIN LATERAL lock_source_observation_payload(
        key.observation_kind, key.schema_version, decode(key.payload_sha256, 'hex'), key.payload
    ) AS stored ON TRUE
), payload_write AS (
    INSERT INTO source_observation_payloads (
        observation_kind, schema_version, canonical_profile, payload_sha256, payload
    )
    SELECT observation_kind, schema_version, 'source-observation-canonical-json-v1',
           decode(payload_sha256, 'hex'), payload
    FROM payload_existing
    WHERE id IS NULL
    ORDER BY observation_kind, schema_version, payload_sha256
    ON CONFLICT (observation_kind, schema_version, canonical_profile, payload_sha256) DO NOTHING
    RETURNING id, observation_kind, schema_version, encode(payload_sha256, 'hex') AS payload_sha256, payload
), payload_resolved AS MATERIALIZED (
    SELECT observation_kind, schema_version, payload_sha256, id, payload
    FROM payload_existing
    WHERE id IS NOT NULL
    UNION ALL
    SELECT observation_kind, schema_version, payload_sha256, id, payload
    FROM payload_write
), payload_resolution AS MATERIALIZED (
    SELECT existing.ordinal,
           assert_source_observation_payload_match(payload_resolved.id, payload_resolved.payload, input.payload) AS id
    FROM existing
    JOIN input ON input.ordinal = existing.ordinal
    JOIN payload_resolved
      ON payload_resolved.observation_kind = existing.observation_kind
     AND payload_resolved.schema_version = existing.schema_version
     AND payload_resolved.payload_sha256 = existing.payload_sha256
    WHERE existing.existing_id IS NULL AND NOT existing.is_collision
), payload_guard AS MATERIALIZED (
    SELECT assert_source_observation_payload_count(
        count(payload_resolution.id), (SELECT count(existing.ordinal) FROM existing WHERE existing_id IS NULL AND NOT is_collision)
    ) AS resolved
    FROM payload_resolution
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
    WHERE existing.is_collision
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
        payload_id,
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
           payload_resolution.id,
           existing.evidence_sha256,
           existing.collector_instance,
           existing.job_key,
           existing.collection_job_kind,
           existing.fence_epoch,
           existing.projection_generation
    FROM existing
    JOIN payload_resolution ON payload_resolution.ordinal = existing.ordinal
    CROSS JOIN payload_guard
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
), previous_checkpoint AS MATERIALIZED (
    -- 같은 statement snapshot은 아래 checkpoint_write의 변경을 보지 않으므로 직전 durable 수락 시각이다.
    SELECT existing.ordinal,
           checkpoint.last_success_at
    FROM existing
    JOIN source_collection_checkpoints AS checkpoint
      ON checkpoint.provider = existing.provider
     AND checkpoint.observation_kind = existing.observation_kind
     AND checkpoint.subject_key = existing.subject_key
     AND checkpoint.scope_sha256 = existing.scope_sha256
    WHERE NOT existing.is_collision
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
    WHERE NOT existing.is_collision
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
    WHERE (
        source_collection_checkpoints.contract_generation,
        source_collection_checkpoints.last_observation_key,
        source_collection_checkpoints.last_evidence_sha256,
        source_collection_checkpoints.last_scheduled_for,
        source_collection_checkpoints.collection_latency_ms,
        source_collection_checkpoints.continuity,
        source_collection_checkpoints.cursor,
        source_collection_checkpoints.last_error_code,
        source_collection_checkpoints.last_error_at
    ) IS DISTINCT FROM (
        EXCLUDED.contract_generation,
        EXCLUDED.last_observation_key,
        EXCLUDED.last_evidence_sha256,
        EXCLUDED.last_scheduled_for,
        EXCLUDED.collection_latency_ms,
        EXCLUDED.continuity,
        EXCLUDED.cursor,
        NULL::text,
        NULL::timestamptz
    )
    RETURNING provider, observation_kind, subject_key, scope_sha256
), effects AS MATERIALIZED (
    SELECT (SELECT count(inserted) FROM collision_write)
         + (SELECT count(observation_id) FROM queue_write)
         + (SELECT count(provider) FROM checkpoint_write) AS affected_count
)
SELECT existing.ordinal,
       COALESCE(existing.existing_id, observation_write.id, 0) AS observation_id,
       existing.is_collision,
       existing.existing_id IS NOT NULL AS existed,
       checkpoint_write.provider IS NOT NULL AS checkpoint_advanced,
       previous_checkpoint.last_success_at,
       NOW() AS accepted_at
FROM existing
CROSS JOIN effects
LEFT JOIN previous_checkpoint
  ON previous_checkpoint.ordinal = existing.ordinal
LEFT JOIN checkpoint_write
  ON NOT existing.is_collision
 AND checkpoint_write.provider = existing.provider
 AND checkpoint_write.observation_kind = existing.observation_kind
 AND checkpoint_write.subject_key = existing.subject_key
 AND checkpoint_write.scope_sha256 = existing.scope_sha256
LEFT JOIN observation_write
  ON observation_write.provider = existing.provider
 AND observation_write.observation_kind = existing.observation_kind
 AND observation_write.subject_key = existing.subject_key
 AND observation_write.observation_key = existing.observation_key
 AND observation_write.schema_version = existing.schema_version
 AND observation_write.contract_generation = existing.contract_generation
ORDER BY existing.ordinal
