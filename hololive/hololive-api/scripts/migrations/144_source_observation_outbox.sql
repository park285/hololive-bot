CREATE TABLE IF NOT EXISTS observation_contract_generations (
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    current_schema_version SMALLINT NOT NULL CHECK (current_schema_version > 0),
    current_generation BIGINT NOT NULL CHECK (current_generation > 0),
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, observation_kind),
    CONSTRAINT chk_observation_contract_provider_vocab CHECK (
        provider IN ('holodex', 'youtubejs', 'hololive_official')
    ),
    CONSTRAINT chk_observation_contract_kind_vocab CHECK (
        observation_kind IN (
            'community_page',
            'video_list',
            'shorts_list',
            'live_snapshot',
            'viewer_sample',
            'channel_stats',
            'channel_profile',
            'channel_photo',
            'schedule_snapshot'
        )
    ),
    CONSTRAINT chk_observation_contract_updated_by CHECK (
        length(updated_by) BETWEEN 1 AND 128
    )
);

INSERT INTO observation_contract_generations (
    provider,
    observation_kind,
    current_schema_version,
    current_generation,
    updated_by
)
VALUES
    ('youtubejs', 'community_page', 1, 1, 'migration-144'),
    ('youtubejs', 'video_list', 1, 1, 'migration-144'),
    ('youtubejs', 'shorts_list', 1, 1, 'migration-144'),
    ('youtubejs', 'live_snapshot', 1, 1, 'migration-144'),
    ('youtubejs', 'viewer_sample', 1, 1, 'migration-144'),
    ('youtubejs', 'channel_stats', 1, 1, 'migration-144'),
    ('youtubejs', 'channel_profile', 1, 1, 'migration-144'),
    ('youtubejs', 'channel_photo', 1, 1, 'migration-144'),
    ('holodex', 'live_snapshot', 1, 1, 'migration-144'),
    ('holodex', 'viewer_sample', 1, 1, 'migration-144'),
    ('holodex', 'schedule_snapshot', 1, 1, 'migration-144'),
    ('holodex', 'channel_stats', 1, 1, 'migration-144'),
    ('holodex', 'channel_profile', 1, 1, 'migration-144'),
    ('holodex', 'channel_photo', 1, 1, 'migration-144'),
    ('hololive_official', 'schedule_snapshot', 1, 1, 'migration-144')
ON CONFLICT (provider, observation_kind) DO NOTHING;

CREATE TABLE IF NOT EXISTS youtube_collection_projection_generations (
    generation BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('STAGING', 'CURRENT', 'RETIRED')),
    row_count INTEGER NOT NULL CHECK (row_count >= 0),
    projection_sha256 TEXT NOT NULL CHECK (projection_sha256 ~ '^[0-9a-f]{64}$'),
    valid_until TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ,
    CONSTRAINT chk_youtube_collection_projection_activation_shape CHECK (
        (status = 'STAGING' AND activated_at IS NULL)
        OR
        (status IN ('CURRENT', 'RETIRED') AND activated_at IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS youtube_collection_targets (
    projection_generation BIGINT NOT NULL
        REFERENCES youtube_collection_projection_generations(generation)
        ON DELETE CASCADE,
    subject_key TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    priority SMALLINT NOT NULL CHECK (priority BETWEEN 0 AND 100),
    poll_interval_ms BIGINT NOT NULL CHECK (poll_interval_ms BETWEEN 1000 AND 86400000),
    enabled BOOLEAN NOT NULL,
    valid_until TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (projection_generation, subject_key, observation_kind),
    CONSTRAINT chk_youtube_collection_target_subject CHECK (
        length(subject_key) BETWEEN 1 AND 256
    ),
    CONSTRAINT chk_youtube_collection_target_kind_vocab CHECK (
        observation_kind IN (
            'community_page', 'video_list', 'shorts_list', 'live_snapshot',
            'viewer_sample', 'channel_stats', 'channel_profile', 'channel_photo',
            'schedule_snapshot'
        )
    )
);

CREATE TABLE IF NOT EXISTS youtube_collection_target_reasons (
    projection_generation BIGINT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    reason_kind TEXT NOT NULL,
    reason_key TEXT NOT NULL,
    PRIMARY KEY (
        projection_generation,
        subject_key,
        observation_kind,
        reason_kind,
        reason_key
    ),
    FOREIGN KEY (projection_generation, subject_key, observation_kind)
        REFERENCES youtube_collection_targets(
            projection_generation,
            subject_key,
            observation_kind
        ) ON DELETE CASCADE,
    CONSTRAINT chk_youtube_collection_target_reason_bounds CHECK (
        length(reason_kind) BETWEEN 1 AND 128
        AND length(reason_key) BETWEEN 1 AND 512
    )
);

CREATE TABLE IF NOT EXISTS youtube_collection_job_leases (
    job_key TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    job_class TEXT NOT NULL CHECK (job_class IN ('GLOBAL', 'SUBJECT')),
    collection_job_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    projection_generation BIGINT NOT NULL
        REFERENCES youtube_collection_projection_generations(generation)
        ON DELETE RESTRICT,
    poll_interval_ms BIGINT NOT NULL CHECK (poll_interval_ms BETWEEN 1000 AND 86400000),
    slot_state TEXT NOT NULL DEFAULT 'IDLE'
        CHECK (slot_state IN ('IDLE', 'ACTIVE', 'DEFERRED')),
    scheduled_for TIMESTAMPTZ NOT NULL,
    next_due_at TIMESTAMPTZ NOT NULL,
    retry_not_before TIMESTAMPTZ,
    fence_epoch BIGINT NOT NULL DEFAULT 0 CHECK (fence_epoch >= 0),
    owner_instance TEXT,
    lease_expires_at TIMESTAMPTZ,
    last_completed_at TIMESTAMPTZ,
    last_error_code TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_youtube_collection_job_provider_vocab CHECK (
        provider IN ('holodex', 'youtubejs', 'hololive_official')
    ),
    CONSTRAINT chk_youtube_collection_job_identity CHECK (
        length(job_key) BETWEEN 1 AND 512
        AND length(collection_job_kind) BETWEEN 1 AND 128
        AND length(subject_key) BETWEEN 1 AND 256
        AND (owner_instance IS NULL OR length(owner_instance) BETWEEN 1 AND 128)
        AND (last_error_code IS NULL OR length(last_error_code) BETWEEN 1 AND 128)
    ),
    CONSTRAINT chk_youtube_collection_job_slot_shape CHECK (
        (slot_state = 'IDLE'
            AND owner_instance IS NULL
            AND lease_expires_at IS NULL
            AND retry_not_before IS NULL)
        OR
        (slot_state = 'ACTIVE'
            AND owner_instance IS NOT NULL
            AND lease_expires_at IS NOT NULL
            AND retry_not_before IS NULL)
        OR
        (slot_state = 'DEFERRED'
            AND owner_instance IS NULL
            AND lease_expires_at IS NULL
            AND retry_not_before IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS source_collection_checkpoints (
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    scope_sha256 TEXT NOT NULL,
    contract_generation BIGINT NOT NULL CHECK (contract_generation > 0),
    last_observation_key TEXT NOT NULL,
    last_evidence_sha256 TEXT NOT NULL,
    last_scheduled_for TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ NOT NULL,
    collection_latency_ms BIGINT NOT NULL CHECK (collection_latency_ms >= 0),
    continuity TEXT NOT NULL,
    cursor JSONB,
    last_error_code TEXT,
    last_error_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, observation_kind, subject_key, scope_sha256),
    CONSTRAINT fk_source_checkpoint_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_checkpoint_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
        AND length(last_observation_key) BETWEEN 1 AND 512
        AND (last_error_code IS NULL OR length(last_error_code) BETWEEN 1 AND 128)
    ),
    CONSTRAINT chk_source_checkpoint_hashes CHECK (
        scope_sha256 ~ '^[0-9a-f]{64}$'
        AND last_evidence_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_source_checkpoint_continuity_vocab CHECK (
        continuity IN ('CONTIGUOUS', 'GAP_UNRESOLVED', 'NOT_APPLICABLE')
    ),
    CONSTRAINT chk_source_checkpoint_cursor CHECK (
        cursor IS NULL
        OR (jsonb_typeof(cursor) = 'object' AND octet_length(cursor::text) <= 16384)
    ),
    CONSTRAINT chk_source_checkpoint_error_shape CHECK (
        (last_error_code IS NULL) = (last_error_at IS NULL)
    )
);

CREATE TABLE IF NOT EXISTS source_observations (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    schema_version SMALLINT NOT NULL CHECK (schema_version > 0),
    contract_generation BIGINT NOT NULL CHECK (contract_generation > 0),
    scheduled_for TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    source_event_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    scope_sha256 TEXT NOT NULL,
    completeness TEXT NOT NULL,
    continuity TEXT NOT NULL,
    payload JSONB NOT NULL,
    payload_sha256 TEXT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    collector_instance TEXT NOT NULL,
    job_key TEXT NOT NULL,
    collection_job_kind TEXT NOT NULL,
    fence_epoch BIGINT NOT NULL CHECK (fence_epoch > 0),
    projection_generation BIGINT NOT NULL CHECK (projection_generation > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_observation_identity UNIQUE (
        provider,
        observation_kind,
        subject_key,
        observation_key,
        schema_version,
        contract_generation
    ),
    CONSTRAINT fk_source_observation_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_observation_text_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
        AND length(observation_key) BETWEEN 1 AND 512
        AND length(collector_instance) BETWEEN 1 AND 128
        AND length(job_key) BETWEEN 1 AND 512
        AND length(collection_job_kind) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_source_observation_payload CHECK (
        jsonb_typeof(payload) = 'object'
        AND octet_length(payload::text) <= 1048576
    ),
    CONSTRAINT chk_source_observation_hashes CHECK (
        scope_sha256 ~ '^[0-9a-f]{64}$'
        AND payload_sha256 ~ '^[0-9a-f]{64}$'
        AND evidence_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_source_observation_completeness_vocab CHECK (
        completeness IN ('COMPLETE', 'PARTIAL', 'UNKNOWN')
    ),
    CONSTRAINT chk_source_observation_continuity_vocab CHECK (
        continuity IN ('CONTIGUOUS', 'GAP_UNRESOLVED', 'NOT_APPLICABLE')
    )
);

CREATE TABLE IF NOT EXISTS source_observation_queue (
    observation_id BIGINT PRIMARY KEY
        REFERENCES source_observations(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempt_count SMALLINT NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 64),
    replay_count SMALLINT NOT NULL DEFAULT 0 CHECK (replay_count BETWEEN 0 AND 16),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner TEXT,
    lease_token TEXT,
    lease_expires_at TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    dead_lettered_at TIMESTAMPTZ,
    last_error_code TEXT,
    last_error_detail TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_source_observation_queue_status_vocab CHECK (
        status IN ('PENDING', 'PROCESSING', 'PROCESSED', 'DEAD_LETTER')
    ),
    CONSTRAINT chk_source_observation_queue_bounds CHECK (
        (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128)
        AND (lease_token IS NULL OR lease_token ~ '^[0-9a-f]{64}$')
        AND (last_error_code IS NULL OR length(last_error_code) BETWEEN 1 AND 128)
        AND (last_error_detail IS NULL OR length(last_error_detail) <= 2048)
    ),
    CONSTRAINT chk_source_observation_queue_lease_shape CHECK (
        (status = 'PROCESSING'
            AND lease_owner IS NOT NULL
            AND lease_token IS NOT NULL
            AND lease_expires_at IS NOT NULL)
        OR
        (status <> 'PROCESSING'
            AND lease_owner IS NULL
            AND lease_token IS NULL
            AND lease_expires_at IS NULL)
    ),
    CONSTRAINT chk_source_observation_queue_terminal_shape CHECK (
        (status = 'PROCESSED') = (processed_at IS NOT NULL)
        AND (status = 'DEAD_LETTER') = (dead_lettered_at IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS source_observation_collisions (
    id BIGSERIAL PRIMARY KEY,
    existing_observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    schema_version SMALLINT NOT NULL CHECK (schema_version > 0),
    contract_generation BIGINT NOT NULL CHECK (contract_generation > 0),
    existing_evidence_sha256 TEXT NOT NULL,
    attempted_evidence_sha256 TEXT NOT NULL,
    attempted_payload_sha256 TEXT NOT NULL,
    collector_instance TEXT NOT NULL,
    job_key TEXT NOT NULL,
    fence_epoch BIGINT NOT NULL CHECK (fence_epoch > 0),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_source_observation_collision_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_observation_collision_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
        AND length(observation_key) BETWEEN 1 AND 512
        AND length(collector_instance) BETWEEN 1 AND 128
        AND length(job_key) BETWEEN 1 AND 512
    ),
    CONSTRAINT chk_source_observation_collision_hashes CHECK (
        existing_evidence_sha256 ~ '^[0-9a-f]{64}$'
        AND attempted_evidence_sha256 ~ '^[0-9a-f]{64}$'
        AND attempted_payload_sha256 ~ '^[0-9a-f]{64}$'
    )
);

CREATE TABLE IF NOT EXISTS source_observation_consumer_offsets (
    consumer_name TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    last_processed_id BIGINT NOT NULL DEFAULT 0 CHECK (last_processed_id >= 0),
    last_effective_at TIMESTAMPTZ,
    last_processed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (consumer_name, observation_kind),
    CONSTRAINT chk_source_observation_consumer_offset_bounds CHECK (
        length(consumer_name) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_source_observation_consumer_offset_kind_vocab CHECK (
        observation_kind IN (
            'community_page', 'video_list', 'shorts_list', 'live_snapshot',
            'viewer_sample', 'channel_stats', 'channel_profile', 'channel_photo',
            'schedule_snapshot'
        )
    )
);

CREATE TABLE IF NOT EXISTS source_observation_replay_requests (
    id BIGSERIAL PRIMARY KEY,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    reason TEXT NOT NULL,
    previous_attempt_count SMALLINT NOT NULL CHECK (previous_attempt_count BETWEEN 0 AND 64),
    status TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'APPLIED', 'REJECTED')),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ,
    rejection_code TEXT,
    CONSTRAINT fk_source_observation_replay_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_observation_replay_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
        AND length(observation_key) BETWEEN 1 AND 512
        AND length(requested_by) BETWEEN 1 AND 128
        AND length(reason) BETWEEN 1 AND 1024
        AND (rejection_code IS NULL OR length(rejection_code) BETWEEN 1 AND 128)
    ),
    CONSTRAINT chk_source_observation_replay_hash CHECK (
        evidence_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_source_observation_replay_terminal_shape CHECK (
        (status = 'APPLIED' AND applied_at IS NOT NULL AND rejection_code IS NULL)
        OR
        (status = 'REJECTED' AND applied_at IS NULL AND rejection_code IS NOT NULL)
        OR
        (status = 'PENDING' AND applied_at IS NULL AND rejection_code IS NULL)
    )
);

CREATE TABLE IF NOT EXISTS source_observation_applications (
    id BIGSERIAL PRIMARY KEY,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    entity_kind TEXT NOT NULL,
    entity_key TEXT NOT NULL,
    decision TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_observation_application UNIQUE (
        observation_id,
        entity_kind,
        entity_key
    ),
    CONSTRAINT fk_source_observation_application_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_observation_application_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
        AND length(entity_kind) BETWEEN 1 AND 64
        AND length(entity_key) BETWEEN 1 AND 256
        AND length(decision) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_source_observation_application_hash CHECK (
        evidence_sha256 ~ '^[0-9a-f]{64}$'
    )
);

CREATE TABLE IF NOT EXISTS source_reconciliation_conflicts (
    id BIGSERIAL PRIMARY KEY,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    entity_kind TEXT NOT NULL,
    entity_key TEXT NOT NULL,
    field_name TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    existing_value_sha256 TEXT NOT NULL,
    attempted_value_sha256 TEXT NOT NULL,
    decision TEXT NOT NULL
        CHECK (decision IN ('KEEP_EXISTING', 'UNRESOLVED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_reconciliation_conflict UNIQUE (
        observation_id,
        entity_kind,
        entity_key,
        field_name
    ),
    CONSTRAINT fk_source_reconciliation_conflict_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_reconciliation_conflict_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
        AND length(observation_key) BETWEEN 1 AND 512
        AND length(entity_kind) BETWEEN 1 AND 64
        AND length(entity_key) BETWEEN 1 AND 256
        AND length(field_name) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_source_reconciliation_conflict_hashes CHECK (
        evidence_sha256 ~ '^[0-9a-f]{64}$'
        AND existing_value_sha256 ~ '^[0-9a-f]{64}$'
        AND attempted_value_sha256 ~ '^[0-9a-f]{64}$'
    )
);

CREATE TABLE IF NOT EXISTS youtube_live_reconciliation_heads (
    video_id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('UPCOMING', 'LIVE', 'ENDED')),
    last_upcoming_positive_at TIMESTAMPTZ,
    last_upcoming_positive_seen_at TIMESTAMPTZ,
    last_live_positive_at TIMESTAMPTZ,
    last_live_positive_seen_at TIMESTAMPTZ,
    last_end_evidence_at TIMESTAMPTZ,
    last_complete_absence_at TIMESTAMPTZ,
    last_absence_scheduled_for TIMESTAMPTZ,
    consecutive_absence_slots SMALLINT NOT NULL DEFAULT 0
        CHECK (consecutive_absence_slots BETWEEN 0 AND 32767),
    end_candidate_kind TEXT
        CHECK (end_candidate_kind IN ('EXPLICIT_END', 'EXPLICIT_CANCEL', 'SCOPED_ABSENCE')),
    end_candidate_observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE RESTRICT,
    next_end_check_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    end_reason TEXT CHECK (
        end_reason IN ('EXPLICIT_END', 'CANCELLED_BEFORE_LIVE', 'SCOPED_ABSENCE')
    ),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_youtube_live_head_video_id CHECK (
        length(video_id) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_youtube_live_head_candidate_shape CHECK (
        (end_candidate_kind IS NULL
            AND end_candidate_observation_id IS NULL
            AND next_end_check_at IS NULL)
        OR
        (end_candidate_kind IS NOT NULL
            AND end_candidate_observation_id IS NOT NULL
            AND next_end_check_at IS NOT NULL)
    )
);

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        IF to_regclass('public.major_events') IS NOT NULL THEN
            EXECUTE 'REVOKE ALL PRIVILEGES ON TABLE public.major_events FROM hololive_scraper';
        END IF;
        IF to_regclass('public.major_event_subscriptions') IS NOT NULL THEN
            EXECUTE 'REVOKE ALL PRIVILEGES ON TABLE public.major_event_subscriptions FROM hololive_scraper';
        END IF;
        IF to_regclass('public.alarms') IS NOT NULL THEN
            EXECUTE 'REVOKE ALL PRIVILEGES ON TABLE public.alarms FROM hololive_scraper';
        END IF;
        IF to_regclass('public.youtube_notification_outbox') IS NOT NULL THEN
            EXECUTE 'REVOKE ALL PRIVILEGES ON TABLE public.youtube_notification_outbox FROM hololive_scraper';
        END IF;
        IF to_regclass('public.major_events_id_seq') IS NOT NULL THEN
            EXECUTE 'REVOKE ALL PRIVILEGES ON SEQUENCE public.major_events_id_seq FROM hololive_scraper';
        END IF;
        REVOKE ALL PRIVILEGES ON TABLE
            observation_contract_generations,
            youtube_collection_projection_generations,
            youtube_collection_targets,
            youtube_collection_target_reasons,
            youtube_collection_job_leases,
            source_collection_checkpoints,
            source_observations,
            source_observation_queue,
            source_observation_collisions,
            source_observation_consumer_offsets,
            source_observation_replay_requests,
            source_observation_applications,
            source_reconciliation_conflicts,
            youtube_live_reconciliation_heads
        FROM hololive_scraper;
        REVOKE ALL PRIVILEGES ON SEQUENCE
            source_observations_id_seq,
            source_observation_collisions_id_seq,
            youtube_collection_projection_generations_generation_seq,
            source_observation_replay_requests_id_seq,
            source_observation_applications_id_seq,
            source_reconciliation_conflicts_id_seq
        FROM hololive_scraper;
        GRANT SELECT ON TABLE observation_contract_generations TO hololive_scraper;
        GRANT SELECT ON TABLE youtube_collection_projection_generations TO hololive_scraper;
        GRANT SELECT ON TABLE youtube_collection_targets TO hololive_scraper;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_collection_job_leases TO hololive_scraper;
        GRANT SELECT, INSERT, UPDATE ON TABLE source_collection_checkpoints TO hololive_scraper;
        GRANT SELECT, INSERT ON TABLE source_observations TO hololive_scraper;
        GRANT SELECT, INSERT ON TABLE source_observation_queue TO hololive_scraper;
        GRANT INSERT ON TABLE source_observation_collisions TO hololive_scraper;
        GRANT USAGE, SELECT ON SEQUENCE source_observations_id_seq TO hololive_scraper;
        GRANT USAGE, SELECT ON SEQUENCE source_observation_collisions_id_seq TO hololive_scraper;
    ELSE
        RAISE NOTICE 'Role hololive_scraper does not exist, skipping source observation grants';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL PRIVILEGES ON TABLE
            observation_contract_generations,
            youtube_collection_projection_generations,
            youtube_collection_targets,
            youtube_collection_target_reasons,
            youtube_collection_job_leases,
            source_collection_checkpoints,
            source_observations,
            source_observation_queue,
            source_observation_collisions,
            source_observation_consumer_offsets,
            source_observation_replay_requests,
            source_observation_applications,
            source_reconciliation_conflicts,
            youtube_live_reconciliation_heads
        FROM hololive_runtime;
        REVOKE ALL PRIVILEGES ON SEQUENCE
            source_observations_id_seq,
            source_observation_collisions_id_seq,
            youtube_collection_projection_generations_generation_seq,
            source_observation_replay_requests_id_seq,
            source_observation_applications_id_seq,
            source_reconciliation_conflicts_id_seq
        FROM hololive_runtime;
        GRANT SELECT ON TABLE observation_contract_generations TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE youtube_collection_projection_generations TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE youtube_collection_targets TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE youtube_collection_target_reasons TO hololive_runtime;
        GRANT SELECT, DELETE ON TABLE source_observations TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE source_observation_queue TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE source_observation_consumer_offsets TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE source_observation_replay_requests TO hololive_runtime;
        GRANT SELECT, INSERT, DELETE ON TABLE source_observation_applications TO hololive_runtime;
        GRANT SELECT, INSERT, DELETE ON TABLE source_reconciliation_conflicts TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_live_reconciliation_heads TO hololive_runtime;
        GRANT SELECT, DELETE ON TABLE source_observation_collisions TO hololive_runtime;
        GRANT USAGE, SELECT ON SEQUENCE youtube_collection_projection_generations_generation_seq TO hololive_runtime;
        GRANT USAGE, SELECT ON SEQUENCE source_observation_replay_requests_id_seq TO hololive_runtime;
        GRANT USAGE, SELECT ON SEQUENCE source_observation_applications_id_seq TO hololive_runtime;
        GRANT USAGE, SELECT ON SEQUENCE source_reconciliation_conflicts_id_seq TO hololive_runtime;
    ELSE
        RAISE NOTICE 'Role hololive_runtime does not exist, skipping source observation runtime grants';
    END IF;
END
$migration$;

COMMENT ON TABLE observation_contract_generations IS 'Current publish contract fence for each provider and observation kind.';
COMMENT ON TABLE source_collection_checkpoints IS 'Collector continuity and health metadata; not content deduplication authority.';
COMMENT ON TABLE source_observations IS 'Immutable normalized source evidence.';
COMMENT ON TABLE source_observation_queue IS 'Mutable processing state for immutable source observations.';
COMMENT ON TABLE source_observation_collisions IS 'Bounded audit for conflicting semantic evidence at one observation identity.';
