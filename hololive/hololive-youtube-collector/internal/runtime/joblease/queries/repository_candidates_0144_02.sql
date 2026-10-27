-- 후보 판정 본문과 generic plan 고정은 migration 343의 youtube_collection_job_candidates가 소유한다.
-- 본문을 바꿀 때는 새 migration으로 함수를 다시 정의한다.
SELECT is_current, subject_key, min_interval_ms, max_interval_ms
FROM public.youtube_collection_job_candidates($1, $2, $3, $4, $5, $6);
