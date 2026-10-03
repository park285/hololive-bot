DELETE FROM youtube_live_viewer_samples
WHERE video_id = $1
  AND captured_at = $2
