SELECT effective_at
FROM youtube_channel_live_checks
WHERE channel_id = $1
  AND channel_identity_confirmed
  AND outcome IN ('UPCOMING_VIDEO', 'CHANNEL_PAGE')
  AND effective_at > $2
  AND effective_at BETWEEN $3::timestamptz - INTERVAL '5 minutes' AND $3::timestamptz + INTERVAL '5 minutes'
