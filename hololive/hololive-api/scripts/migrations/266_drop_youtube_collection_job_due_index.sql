-- lease 행은 모두 job_key PK나 projection_generation 인덱스로 찾으므로 이 인덱스를 읽는 런타임 SQL이 없습니다.
-- renew가 key 컬럼 lease_expires_at을 바꿔 HOT 갱신을 막으므로 제거합니다(운영 판정: db-hotpath 계획 B2).
DROP INDEX CONCURRENTLY IF EXISTS public.idx_youtube_collection_job_due;
