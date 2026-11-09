SELECT is_current, subject_key, min_interval_ms, max_interval_ms
FROM public.youtube_collection_job_candidates($1, $2, $3, $4, $5, $6);
