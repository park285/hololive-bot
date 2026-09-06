CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_live_absence_slots_channels
    ON youtube_live_absence_slots USING gin ((coverage -> 'requested_channel_ids'));
