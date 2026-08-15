SELECT channel_id
FROM members
WHERE channel_id IS NOT NULL
  AND btrim(channel_id) <> ''
  AND COALESCE(is_graduated, FALSE) = FALSE
ORDER BY english_name, channel_id
LIMIT $1
