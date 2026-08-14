CREATE TABLE IF NOT EXISTS youtube_content_evidence_clocks (
    video_id VARCHAR(20) PRIMARY KEY
        REFERENCES youtube_videos(video_id) ON DELETE CASCADE,
    first_positive_effective_at TIMESTAMPTZ NOT NULL,
    last_positive_effective_at TIMESTAMPTZ NOT NULL,
    last_positive_received_at TIMESTAMPTZ NOT NULL,
    last_positive_value_sha256 TEXT NOT NULL,
    last_positive_scope_sha256 TEXT NOT NULL,
    last_positive_coverage JSONB NOT NULL,
    last_negative_effective_at TIMESTAMPTZ,
    last_negative_received_at TIMESTAMPTZ,
    first_absence_scheduled_for TIMESTAMPTZ,
    second_absence_scheduled_for TIMESTAMPTZ,
    last_absence_observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    missing_since_effective_at TIMESTAMPTZ,
    consecutive_absence_slots SMALLINT NOT NULL DEFAULT 0
        CHECK (consecutive_absence_slots BETWEEN 0 AND 32767),
    withdrawn_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_youtube_content_clock_video_id CHECK (
        length(video_id) BETWEEN 1 AND 20
    ),
    CONSTRAINT chk_youtube_content_clock_hashes CHECK (
        last_positive_value_sha256 ~ '^[0-9a-f]{64}$'
        AND last_positive_scope_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_youtube_content_clock_coverage CHECK (
        jsonb_typeof(last_positive_coverage) = 'object'
        AND octet_length(last_positive_coverage::text) <= 8192
    )
);

CREATE TABLE IF NOT EXISTS youtube_content_absence_slots (
    channel_id VARCHAR(50) NOT NULL,
    observation_kind TEXT NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    evidence_sha256 TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    scope_sha256 TEXT NOT NULL,
    coverage JSONB NOT NULL,
    PRIMARY KEY (channel_id, observation_kind, scheduled_for),
    CONSTRAINT chk_youtube_content_absence_kind CHECK (
        observation_kind IN ('video_list', 'shorts_list')
    ),
    CONSTRAINT chk_youtube_content_absence_bounds CHECK (
        length(channel_id) BETWEEN 1 AND 50
    ),
    CONSTRAINT chk_youtube_content_absence_hashes CHECK (
        evidence_sha256 ~ '^[0-9a-f]{64}$'
        AND scope_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_youtube_content_absence_coverage CHECK (
        jsonb_typeof(coverage) = 'object'
        AND octet_length(coverage::text) <= 8192
    )
);

CREATE TABLE IF NOT EXISTS youtube_content_channel_heads (
    channel_id VARCHAR(50) NOT NULL,
    observation_kind TEXT NOT NULL,
    earliest_complete_effective_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, observation_kind),
    CONSTRAINT chk_youtube_content_channel_head_kind CHECK (
        observation_kind IN ('video_list', 'shorts_list')
    ),
    CONSTRAINT chk_youtube_content_channel_head_bounds CHECK (
        length(channel_id) BETWEEN 1 AND 50
    )
);

REVOKE ALL ON TABLE youtube_content_evidence_clocks FROM PUBLIC;
REVOKE ALL ON TABLE youtube_content_absence_slots FROM PUBLIC;
REVOKE ALL ON TABLE youtube_content_channel_heads FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE youtube_content_evidence_clocks FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_content_absence_slots FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_content_channel_heads FROM hololive_scraper;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE youtube_content_evidence_clocks FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_content_absence_slots FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_content_channel_heads FROM hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_content_evidence_clocks TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_content_absence_slots TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_content_channel_heads TO hololive_runtime;
    END IF;
END
$migration$;
