INSERT INTO youtube_live_viewer_sample_heads (
    video_id, last_resolved_window_start, last_resolved_count, last_resolved_availability,
    prior_resolved_window_start, prior_resolved_count, prior_resolved_availability,
    unresolved_window_start
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (video_id) DO UPDATE SET
    last_resolved_window_start = excluded.last_resolved_window_start,
    last_resolved_count = excluded.last_resolved_count,
    last_resolved_availability = excluded.last_resolved_availability,
    prior_resolved_window_start = excluded.prior_resolved_window_start,
    prior_resolved_count = excluded.prior_resolved_count,
    prior_resolved_availability = excluded.prior_resolved_availability,
    unresolved_window_start = excluded.unresolved_window_start,
    updated_at = NOW()
