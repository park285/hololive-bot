SELECT channel_id
FROM members
WHERE channel_id IS NOT NULL
  AND btrim(channel_id) <> ''
  AND is_graduated = FALSE
ORDER BY english_name, channel_id
LIMIT $1
