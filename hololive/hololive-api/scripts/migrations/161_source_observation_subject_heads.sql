CREATE TABLE IF NOT EXISTS source_observation_subject_heads (
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    source_observation_id BIGINT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, observation_kind, subject_key),
    CONSTRAINT fk_source_observation_subject_head_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_observation_subject_head_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
    ),
    CONSTRAINT chk_source_observation_subject_head_hash CHECK (
        evidence_sha256 ~ '^[0-9a-f]{64}$'
    )
);

REVOKE ALL ON TABLE source_observation_subject_heads FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE source_observation_subject_heads FROM hololive_scraper;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE source_observation_subject_heads FROM hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE source_observation_subject_heads TO hololive_runtime;
    END IF;
END
$migration$;
