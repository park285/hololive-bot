CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_alarms_room_channel_host
    ON public.alarms (room_id, channel_id, host_id);
