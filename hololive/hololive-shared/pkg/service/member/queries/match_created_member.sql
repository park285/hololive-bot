SELECT id
FROM members
WHERE slug = $1
  AND channel_id IS NOT DISTINCT FROM NULLIF($2, '')
  AND english_name = $3
  AND japanese_name IS NOT DISTINCT FROM NULLIF($4, '')
  AND korean_name IS NOT DISTINCT FROM NULLIF($5, '')
  AND status = $6
  AND is_graduated = $7
  AND aliases = $8::jsonb
  AND org = $9
  AND suborg IS NULL
  AND sync_source = $10
  AND units = COALESCE($11::text[], '{}'::text[])
  AND official_link IS NOT DISTINCT FROM NULLIF($12, '')
  AND birthday IS NOT DISTINCT FROM $13::date
  AND debut_date IS NOT DISTINCT FROM $14::date
  AND short_korean_name IS NOT DISTINCT FROM NULLIF($15, '')
  AND chzzk_channel_id IS NOT DISTINCT FROM NULLIF($16, '')
  AND twitch_user_id IS NOT DISTINCT FROM NULLIF($17, '')
