ALTER TABLE youtube_live_sessions
    ADD COLUMN IF NOT EXISTS topic_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS thumbnail_url TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN youtube_live_sessions.topic_id IS 'Raw YouTube/Holodex stream topic id as observed by live polling or alarm payload.';
COMMENT ON COLUMN youtube_live_sessions.thumbnail_url IS 'Best observed stream thumbnail URL for bot display/download.';
