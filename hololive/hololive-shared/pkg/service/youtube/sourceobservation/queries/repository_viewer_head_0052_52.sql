SELECT video_id, last_resolved_window_start, last_resolved_count, last_resolved_availability,
    prior_resolved_window_start, prior_resolved_count, prior_resolved_availability,
    unresolved_window_start
FROM youtube_live_viewer_sample_heads
WHERE video_id = $1
FOR UPDATE
