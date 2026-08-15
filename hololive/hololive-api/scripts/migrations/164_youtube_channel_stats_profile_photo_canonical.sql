ALTER TABLE youtube_channel_stats_snapshots
    ALTER COLUMN subscriber_count DROP DEFAULT,
    ALTER COLUMN view_count DROP DEFAULT,
    ALTER COLUMN video_count DROP DEFAULT,
    ALTER COLUMN subscriber_count DROP NOT NULL,
    ALTER COLUMN view_count DROP NOT NULL,
    ALTER COLUMN video_count DROP NOT NULL;

ALTER TABLE youtube_channel_stats_snapshots
    DROP CONSTRAINT IF EXISTS chk_ycss_counts_nonneg;
ALTER TABLE youtube_channel_stats_snapshots
    ADD CONSTRAINT chk_ycss_counts_nonneg CHECK (
        (subscriber_count IS NULL OR subscriber_count >= 0)
        AND (view_count IS NULL OR view_count >= 0)
        AND (video_count IS NULL OR video_count >= 0)
    ) NOT VALID;
ALTER TABLE youtube_channel_stats_snapshots
    VALIDATE CONSTRAINT chk_ycss_counts_nonneg;

CREATE TABLE IF NOT EXISTS youtube_channel_stats_evidence (
    channel_id TEXT NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    subscriber_count BIGINT,
    view_count BIGINT,
    video_count BIGINT,
    subscriber_covered BOOLEAN NOT NULL,
    view_covered BOOLEAN NOT NULL,
    video_covered BOOLEAN NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (channel_id, scheduled_for, provider),
    CONSTRAINT chk_youtube_stats_evidence_channel CHECK (
        length(channel_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT chk_youtube_stats_evidence_provider CHECK (
        provider IN ('youtubejs', 'holodex', 'hololive_official')
    ),
    CONSTRAINT chk_youtube_stats_evidence_counts CHECK (
        (subscriber_count IS NULL OR subscriber_count >= 0)
        AND (view_count IS NULL OR view_count >= 0)
        AND (video_count IS NULL OR video_count >= 0)
    )
);

CREATE TABLE IF NOT EXISTS youtube_channel_stats_heads (
    channel_id TEXT PRIMARY KEY,
    last_resolved_scheduled_for TIMESTAMPTZ,
    last_resolved_subscriber_count BIGINT,
    last_resolved_view_count BIGINT,
    last_resolved_video_count BIGINT,
    prior_resolved_scheduled_for TIMESTAMPTZ,
    prior_resolved_subscriber_count BIGINT,
    prior_resolved_view_count BIGINT,
    prior_resolved_video_count BIGINT,
    unresolved_scheduled_for TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_youtube_stats_head_channel CHECK (
        length(channel_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT chk_youtube_stats_head_counts CHECK (
        (last_resolved_subscriber_count IS NULL OR last_resolved_subscriber_count >= 0)
        AND (last_resolved_view_count IS NULL OR last_resolved_view_count >= 0)
        AND (last_resolved_video_count IS NULL OR last_resolved_video_count >= 0)
        AND (prior_resolved_subscriber_count IS NULL OR prior_resolved_subscriber_count >= 0)
        AND (prior_resolved_view_count IS NULL OR prior_resolved_view_count >= 0)
        AND (prior_resolved_video_count IS NULL OR prior_resolved_video_count >= 0)
    )
);

CREATE TABLE IF NOT EXISTS youtube_channel_profile_evidence (
    channel_id TEXT NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    handle_present BOOLEAN NOT NULL,
    handle TEXT NOT NULL DEFAULT '',
    description_present BOOLEAN NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    country_present BOOLEAN NOT NULL,
    country TEXT NOT NULL DEFAULT '',
    joined_date_present BOOLEAN NOT NULL,
    joined_date TEXT NOT NULL DEFAULT '',
    complete BOOLEAN NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (channel_id, scheduled_for, provider),
    CONSTRAINT chk_youtube_profile_evidence_channel CHECK (
        length(channel_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT chk_youtube_profile_evidence_provider CHECK (
        provider IN ('youtubejs', 'holodex', 'hololive_official')
    ),
    CONSTRAINT chk_youtube_profile_evidence_bounds CHECK (
        length(handle) <= 256
        AND octet_length(description) <= 4096
        AND length(country) <= 50
        AND length(joined_date) <= 256
    )
);

CREATE TABLE IF NOT EXISTS youtube_channel_profile_heads (
    channel_id TEXT PRIMARY KEY,
    handle_set BOOLEAN NOT NULL DEFAULT FALSE,
    handle TEXT NOT NULL DEFAULT '',
    handle_effective_at TIMESTAMPTZ,
    description_set BOOLEAN NOT NULL DEFAULT FALSE,
    description TEXT NOT NULL DEFAULT '',
    description_effective_at TIMESTAMPTZ,
    description_empty_slots SMALLINT NOT NULL DEFAULT 0,
    description_empty_first_scheduled_for TIMESTAMPTZ,
    description_empty_last_scheduled_for TIMESTAMPTZ,
    description_empty_first_received_at TIMESTAMPTZ,
    country_set BOOLEAN NOT NULL DEFAULT FALSE,
    country TEXT NOT NULL DEFAULT '',
    country_effective_at TIMESTAMPTZ,
    country_empty_slots SMALLINT NOT NULL DEFAULT 0,
    country_empty_first_scheduled_for TIMESTAMPTZ,
    country_empty_last_scheduled_for TIMESTAMPTZ,
    country_empty_first_received_at TIMESTAMPTZ,
    joined_date_set BOOLEAN NOT NULL DEFAULT FALSE,
    joined_date TEXT NOT NULL DEFAULT '',
    joined_date_effective_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_youtube_profile_head_channel CHECK (
        length(channel_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT chk_youtube_profile_head_bounds CHECK (
        length(handle) <= 256
        AND octet_length(description) <= 4096
        AND length(country) <= 50
        AND length(joined_date) <= 256
        AND description_empty_slots BETWEEN 0 AND 32767
        AND country_empty_slots BETWEEN 0 AND 32767
    )
);

CREATE TABLE IF NOT EXISTS youtube_channel_photo_variants (
    channel_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    provider TEXT NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    url TEXT NOT NULL,
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    stable_media_id TEXT NOT NULL DEFAULT '',
    content_fingerprint TEXT NOT NULL DEFAULT '',
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (channel_id, kind, provider, scheduled_for),
    CONSTRAINT chk_youtube_photo_variant_channel CHECK (
        length(channel_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT chk_youtube_photo_variant_provider CHECK (
        provider IN ('youtubejs', 'holodex', 'hololive_official')
    ),
    CONSTRAINT chk_youtube_photo_variant_kind CHECK (
        kind IN ('avatar', 'banner')
    ),
    CONSTRAINT chk_youtube_photo_variant_url CHECK (
        length(url) BETWEEN 8 AND 2048
        AND url LIKE 'https://%'
    ),
    CONSTRAINT chk_youtube_photo_variant_dims CHECK (
        width BETWEEN 0 AND 20000
        AND height BETWEEN 0 AND 20000
    ),
    CONSTRAINT chk_youtube_photo_variant_identity CHECK (
        length(stable_media_id) <= 512
        AND (
            content_fingerprint = ''
            OR content_fingerprint ~ '^[0-9a-f]{64}$'
        )
    )
);

CREATE TABLE IF NOT EXISTS youtube_channel_photo_heads (
    channel_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    identity TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    effective_at TIMESTAMPTZ,
    candidate_identity TEXT NOT NULL DEFAULT '',
    candidate_url TEXT NOT NULL DEFAULT '',
    candidate_width INTEGER NOT NULL DEFAULT 0,
    candidate_height INTEGER NOT NULL DEFAULT 0,
    candidate_slots SMALLINT NOT NULL DEFAULT 0,
    candidate_first_scheduled_for TIMESTAMPTZ,
    candidate_last_scheduled_for TIMESTAMPTZ,
    candidate_first_received_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, kind),
    CONSTRAINT chk_youtube_photo_head_channel CHECK (
        length(channel_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT chk_youtube_photo_head_kind CHECK (
        kind IN ('avatar', 'banner')
    ),
    CONSTRAINT chk_youtube_photo_head_bounds CHECK (
        length(identity) <= 520
        AND length(url) <= 2048
        AND length(candidate_identity) <= 520
        AND length(candidate_url) <= 2048
        AND width BETWEEN 0 AND 20000
        AND height BETWEEN 0 AND 20000
        AND candidate_width BETWEEN 0 AND 20000
        AND candidate_height BETWEEN 0 AND 20000
        AND candidate_slots BETWEEN 0 AND 32767
    )
);

REVOKE ALL ON TABLE youtube_channel_stats_evidence FROM PUBLIC;
REVOKE ALL ON TABLE youtube_channel_stats_heads FROM PUBLIC;
REVOKE ALL ON TABLE youtube_channel_profile_evidence FROM PUBLIC;
REVOKE ALL ON TABLE youtube_channel_profile_heads FROM PUBLIC;
REVOKE ALL ON TABLE youtube_channel_photo_variants FROM PUBLIC;
REVOKE ALL ON TABLE youtube_channel_photo_heads FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE youtube_channel_stats_evidence FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_channel_stats_heads FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_channel_profile_evidence FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_channel_profile_heads FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_channel_photo_variants FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_channel_photo_heads FROM hololive_scraper;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE youtube_channel_stats_evidence FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_channel_stats_heads FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_channel_profile_evidence FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_channel_profile_heads FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_channel_photo_variants FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_channel_photo_heads FROM hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_channel_stats_evidence TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_channel_stats_heads TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE youtube_channel_stats_snapshots TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_channel_profile_evidence TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_channel_profile_heads TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_channel_photo_variants TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_channel_photo_heads TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_channel_profiles TO hololive_runtime;
    END IF;
END
$migration$;
