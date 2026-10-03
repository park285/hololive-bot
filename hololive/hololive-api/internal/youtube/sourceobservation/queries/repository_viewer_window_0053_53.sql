SELECT provider, viewer_count, availability
FROM youtube_live_viewer_sample_evidence
WHERE video_id = $1
  AND sample_window_start = $2
FOR UPDATE
