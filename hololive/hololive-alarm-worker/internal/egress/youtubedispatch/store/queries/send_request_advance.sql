UPDATE youtube_notification_send_request SET generation = generation + 1
WHERE base_id = $1 AND generation = $2 AND generation < 2;
