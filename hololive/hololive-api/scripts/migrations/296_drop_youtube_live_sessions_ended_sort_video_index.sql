-- 292의 idx_yls_ended_history_sort가 같은 순서를 제공한다. 이 식 인덱스는 last_seen_at을 참조해 HOT 갱신을 막는다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_yls_ended_sort_video;
