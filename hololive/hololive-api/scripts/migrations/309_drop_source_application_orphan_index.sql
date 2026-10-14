-- 원본 수명 CASCADE 전환 전에 고아 전용 인덱스를 별도 autocommit 문장으로 제거합니다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_source_observation_applications_orphaned_kind_applied_id;
