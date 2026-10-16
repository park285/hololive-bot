-- 319가 만든 코드 열 UNIQUE index로 대체된 text 열 index를 별도 autocommit 문장으로 제거한다.
DROP INDEX CONCURRENTLY IF EXISTS public.uq_source_observation_application_active;
