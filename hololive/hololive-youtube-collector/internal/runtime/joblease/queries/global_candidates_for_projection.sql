-- 후보 판정과 다음 due 힌트는 DB 함수가 같은 snapshot에서 계산한다.
SELECT is_current, subject_key, min_interval_ms, max_interval_ms, next_due_at
FROM public.youtube_collection_job_global_candidates($1, $2, $3, $4, $5, $6);
