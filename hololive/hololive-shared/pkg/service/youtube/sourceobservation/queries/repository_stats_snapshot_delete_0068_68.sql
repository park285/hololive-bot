DELETE FROM youtube_channel_stats_snapshots
WHERE channel_id = $1
  AND captured_at = $2
