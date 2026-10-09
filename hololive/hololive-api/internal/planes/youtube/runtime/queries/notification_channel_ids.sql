SELECT DISTINCT alarms.channel_id
FROM alarms
JOIN members ON members.channel_id = alarms.channel_id
WHERE alarms.channel_id IS NOT NULL
  AND btrim(alarms.channel_id) <> ''
  AND members.is_graduated = FALSE
ORDER BY alarms.channel_id
LIMIT $1
