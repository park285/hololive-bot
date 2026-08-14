CREATE TABLE IF NOT EXISTS youtube_live_viewer_sample_evidence (
    video_id TEXT NOT NULL,
    sample_window_start TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    viewer_count BIGINT,
    availability TEXT NOT NULL,
    sample_window_seconds INTEGER NOT NULL,
    scheduled_for TIMESTAMPTZ NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (video_id, sample_window_start, provider),
    CONSTRAINT chk_youtube_viewer_evidence_video_id CHECK (
        length(video_id) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_youtube_viewer_evidence_provider CHECK (
        provider IN ('youtubejs', 'holodex', 'hololive_official')
    ),
    CONSTRAINT chk_youtube_viewer_evidence_availability CHECK (
        availability IN ('AVAILABLE', 'HIDDEN', 'UNAVAILABLE')
    ),
    CONSTRAINT chk_youtube_viewer_evidence_window CHECK (
        sample_window_seconds BETWEEN 1 AND 86400
    )
);

CREATE TABLE IF NOT EXISTS youtube_live_viewer_sample_heads (
    video_id TEXT PRIMARY KEY,
    last_resolved_window_start TIMESTAMPTZ,
    last_resolved_count BIGINT,
    last_resolved_availability TEXT,
    prior_resolved_window_start TIMESTAMPTZ,
    prior_resolved_count BIGINT,
    prior_resolved_availability TEXT,
    unresolved_window_start TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_youtube_viewer_head_video_id CHECK (
        length(video_id) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_youtube_viewer_head_availability CHECK (
        last_resolved_availability IS NULL
        OR last_resolved_availability IN ('AVAILABLE', 'HIDDEN', 'UNAVAILABLE')
    )
);

CREATE TABLE IF NOT EXISTS youtube_schedule_items (
    group_key TEXT NOT NULL,
    provider TEXT NOT NULL,
    external_id TEXT NOT NULL,
    video_id TEXT NOT NULL DEFAULT '',
    channel_id TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    scheduled_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    is_live BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_key, provider, external_id),
    CONSTRAINT chk_youtube_schedule_item_bounds CHECK (
        length(group_key) BETWEEN 1 AND 256
        AND length(external_id) BETWEEN 1 AND 256
        AND length(video_id) <= 128
        AND length(channel_id) <= 256
        AND length(title) BETWEEN 1 AND 4096
    ),
    CONSTRAINT chk_youtube_schedule_item_provider CHECK (
        provider IN ('youtubejs', 'holodex', 'hololive_official')
    )
);

REVOKE ALL ON TABLE youtube_live_viewer_sample_evidence FROM PUBLIC;
REVOKE ALL ON TABLE youtube_live_viewer_sample_heads FROM PUBLIC;
REVOKE ALL ON TABLE youtube_schedule_items FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE youtube_live_viewer_sample_evidence FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_live_viewer_sample_heads FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_schedule_items FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_live_sessions FROM hololive_scraper;
        REVOKE ALL ON TABLE youtube_live_viewer_samples FROM hololive_scraper;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE youtube_live_viewer_sample_evidence FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_live_viewer_sample_heads FROM hololive_runtime;
        REVOKE ALL ON TABLE youtube_schedule_items FROM hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_live_viewer_sample_evidence TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_live_viewer_sample_heads TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_schedule_items TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE ON TABLE youtube_live_sessions TO hololive_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE youtube_live_viewer_samples TO hololive_runtime;
    END IF;
END
$migration$;
