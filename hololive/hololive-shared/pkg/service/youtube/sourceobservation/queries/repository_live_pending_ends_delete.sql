DELETE FROM youtube_live_pending_ends
WHERE video_id = ANY($1::text[]) AND NOT (video_id = ANY($2::text[]))
