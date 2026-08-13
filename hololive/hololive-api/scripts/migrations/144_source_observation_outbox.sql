CREATE TABLE IF NOT EXISTS source_authority_fences (
    source_kind TEXT PRIMARY KEY,
    mode TEXT NOT NULL,
    generation BIGINT NOT NULL,
    updated_by TEXT NOT NULL DEFAULT 'migration',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_source_authority_fences_source_kind
        CHECK (length(source_kind) BETWEEN 1 AND 64),
    CONSTRAINT chk_source_authority_fences_mode_vocab
        CHECK (mode IN ('legacy', 'shadow', 'authoritative')),
    CONSTRAINT chk_source_authority_fences_generation
        CHECK (generation > 0),
    CONSTRAINT chk_source_authority_fences_updated_by
        CHECK (length(updated_by) BETWEEN 1 AND 128)
);

INSERT INTO source_authority_fences (source_kind, mode, generation, updated_by)
VALUES ('youtube_community', 'legacy', 1, 'migration-144')
ON CONFLICT (source_kind) DO NOTHING;

CREATE TABLE IF NOT EXISTS source_collection_checkpoints (
    source_kind TEXT NOT NULL,
    source_key TEXT NOT NULL,
    generation BIGINT NOT NULL,
    observation_key TEXT NOT NULL,
    payload_sha256 TEXT NOT NULL,
    completeness TEXT NOT NULL,
    continuity TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ NOT NULL,
    collection_latency_ms BIGINT NOT NULL DEFAULT 0,
    last_error_code TEXT,
    last_error_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (source_kind, source_key),
    CONSTRAINT fk_source_collection_checkpoints_authority
        FOREIGN KEY (source_kind)
        REFERENCES source_authority_fences (source_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_collection_checkpoints_source_key
        CHECK (length(source_key) BETWEEN 1 AND 256),
    CONSTRAINT chk_source_collection_checkpoints_generation
        CHECK (generation > 0),
    CONSTRAINT chk_source_collection_checkpoints_observation_key
        CHECK (length(observation_key) BETWEEN 1 AND 512),
    CONSTRAINT chk_source_collection_checkpoints_payload_sha256
        CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_source_collection_checkpoints_completeness_vocab
        CHECK (completeness IN ('COMPLETE_WINDOW', 'PARTIAL_WINDOW')),
    CONSTRAINT chk_source_collection_checkpoints_continuity_vocab
        CHECK (continuity IN ('CONTIGUOUS', 'GAP_UNRESOLVED')),
    CONSTRAINT chk_source_collection_checkpoints_latency
        CHECK (collection_latency_ms >= 0),
    CONSTRAINT chk_source_collection_checkpoints_last_error_code
        CHECK (last_error_code IS NULL OR length(last_error_code) BETWEEN 1 AND 128),
    CONSTRAINT chk_source_collection_checkpoints_last_error_shape
        CHECK ((last_error_code IS NULL) = (last_error_at IS NULL))
);

CREATE TABLE IF NOT EXISTS source_observation_outbox (
    id BIGSERIAL PRIMARY KEY,
    source_kind TEXT NOT NULL,
    source_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    schema_version SMALLINT NOT NULL,
    generation BIGINT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    completeness TEXT NOT NULL,
    continuity TEXT NOT NULL,
    payload JSONB NOT NULL,
    payload_sha256 TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner TEXT,
    lease_token TEXT,
    lease_expires_at TIMESTAMPTZ,
    parity_status TEXT NOT NULL DEFAULT 'NOT_CHECKED',
    parity_detail JSONB,
    processed_at TIMESTAMPTZ,
    dead_lettered_at TIMESTAMPTZ,
    last_error_code TEXT,
    last_error_detail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_source_observation_outbox_authority
        FOREIGN KEY (source_kind)
        REFERENCES source_authority_fences (source_kind)
        ON DELETE RESTRICT,
    CONSTRAINT uq_source_observation_outbox_identity
        UNIQUE (source_kind, source_key, observation_key, schema_version),
    CONSTRAINT chk_source_observation_outbox_source_key
        CHECK (length(source_key) BETWEEN 1 AND 256),
    CONSTRAINT chk_source_observation_outbox_observation_key
        CHECK (length(observation_key) BETWEEN 1 AND 512),
    CONSTRAINT chk_source_observation_outbox_schema_version
        CHECK (schema_version > 0),
    CONSTRAINT chk_source_observation_outbox_generation
        CHECK (generation > 0),
    CONSTRAINT chk_source_observation_outbox_completeness_vocab
        CHECK (completeness IN ('COMPLETE_WINDOW', 'PARTIAL_WINDOW')),
    CONSTRAINT chk_source_observation_outbox_continuity_vocab
        CHECK (continuity IN ('CONTIGUOUS', 'GAP_UNRESOLVED')),
    CONSTRAINT chk_source_observation_outbox_payload_object
        CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT chk_source_observation_outbox_payload_size
        CHECK (octet_length(payload::text) <= 1048576),
    CONSTRAINT chk_source_observation_outbox_payload_sha256
        CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_source_observation_outbox_status_vocab
        CHECK (status IN ('PENDING', 'PROCESSING', 'PROCESSED', 'DEAD_LETTER')),
    CONSTRAINT chk_source_observation_outbox_attempt_count
        CHECK (attempt_count BETWEEN 0 AND 1000000),
    CONSTRAINT chk_source_observation_outbox_lease_owner
        CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),
    CONSTRAINT chk_source_observation_outbox_lease_token
        CHECK (lease_token IS NULL OR lease_token ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_source_observation_outbox_lease_shape
        CHECK (
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
    CONSTRAINT chk_source_observation_outbox_parity_vocab
        CHECK (parity_status IN ('NOT_CHECKED', 'MATCH', 'MISMATCH')),
    CONSTRAINT chk_source_observation_outbox_parity_detail
        CHECK (parity_detail IS NULL OR jsonb_typeof(parity_detail) = 'object'),
    CONSTRAINT chk_source_observation_outbox_processed_shape
        CHECK ((status = 'PROCESSED') = (processed_at IS NOT NULL)),
    CONSTRAINT chk_source_observation_outbox_dead_letter_shape
        CHECK ((status = 'DEAD_LETTER') = (dead_lettered_at IS NOT NULL)),
    CONSTRAINT chk_source_observation_outbox_last_error_code
        CHECK (last_error_code IS NULL OR length(last_error_code) BETWEEN 1 AND 128),
    CONSTRAINT chk_source_observation_outbox_last_error_detail
        CHECK (last_error_detail IS NULL OR length(last_error_detail) <= 2048)
);

CREATE TABLE IF NOT EXISTS source_observation_consumer_offsets (
    consumer_name TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    last_processed_id BIGINT NOT NULL DEFAULT 0,
    last_observed_at TIMESTAMPTZ,
    last_processed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (consumer_name, source_kind),
    CONSTRAINT fk_source_observation_consumer_offsets_authority
        FOREIGN KEY (source_kind)
        REFERENCES source_authority_fences (source_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_observation_consumer_offsets_consumer_name
        CHECK (length(consumer_name) BETWEEN 1 AND 128),
    CONSTRAINT chk_source_observation_consumer_offsets_last_processed_id
        CHECK (last_processed_id >= 0)
);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_outbox_claim
    ON source_observation_outbox (source_kind, available_at, id)
    WHERE status = 'PENDING';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_outbox_lease_recovery
    ON source_observation_outbox (source_kind, lease_expires_at, id)
    WHERE status = 'PROCESSING';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_observation_outbox_terminal_retention
    ON source_observation_outbox (source_kind, updated_at, id)
    WHERE status IN ('PROCESSED', 'DEAD_LETTER');

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT ON TABLE alarms TO hololive_scraper;
        GRANT SELECT ON TABLE source_authority_fences TO hololive_scraper;
        GRANT SELECT, INSERT, UPDATE ON TABLE source_collection_checkpoints TO hololive_scraper;
        GRANT SELECT, INSERT, UPDATE ON TABLE source_observation_outbox TO hololive_scraper;
        GRANT USAGE, SELECT ON SEQUENCE source_observation_outbox_id_seq TO hololive_scraper;
    ELSE
        RAISE NOTICE 'Role hololive_scraper does not exist, skipping source observation grants';
    END IF;
END
$migration$;

COMMENT ON TABLE source_authority_fences IS 'Per-source legacy, shadow, or authoritative ownership fence with monotonic generation.';
COMMENT ON TABLE source_collection_checkpoints IS 'Collector-owned source continuity checkpoint; distinct from producer domain watermarks.';
COMMENT ON TABLE source_observation_outbox IS 'Immutable normalized source observations claimed and finalized by the producer.';
COMMENT ON TABLE source_observation_consumer_offsets IS 'Consumer progress telemetry; individual observation state remains the correctness source.';
