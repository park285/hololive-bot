SELECT count(entries.room_id) FROM (SELECT DISTINCT room_id, channel_id FROM alarms) AS entries
