CREATE TABLE IF NOT EXISTS source_observation_replay_epoch (
    singleton boolean PRIMARY KEY DEFAULT true,
    cutoff_received_at timestamptz NOT NULL,
    activated_by text NOT NULL,
    reason text NOT NULL,
    CONSTRAINT chk_source_observation_replay_epoch_singleton
        CHECK (singleton),
    CONSTRAINT chk_source_observation_replay_epoch_attribution
        CHECK (
            length(btrim(activated_by)) BETWEEN 1 AND 128
            AND activated_by = btrim(activated_by)
            AND length(btrim(reason)) BETWEEN 1 AND 1024
            AND reason = btrim(reason)
        )
);

REVOKE UPDATE, DELETE, TRUNCATE ON TABLE source_observation_replay_epoch FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE source_observation_replay_epoch FROM hololive_scraper;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE UPDATE, DELETE, TRUNCATE ON TABLE source_observation_replay_epoch FROM hololive_runtime;
        GRANT SELECT, INSERT ON TABLE source_observation_replay_epoch TO hololive_runtime;
    END IF;
END
$migration$;
