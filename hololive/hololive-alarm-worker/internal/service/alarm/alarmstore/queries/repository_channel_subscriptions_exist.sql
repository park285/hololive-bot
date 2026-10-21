SELECT EXISTS (SELECT 1 FROM alarms WHERE channel_id = $1)
