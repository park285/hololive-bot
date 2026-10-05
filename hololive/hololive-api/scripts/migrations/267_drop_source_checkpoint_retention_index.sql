-- checkpoint upsert는 매번 updated_at을 바꾸므로 이 인덱스가 HOT 갱신을 막습니다. 보존 삭제는 현행 운영 분포에서
-- 이미 seq scan을 고르고, scope가 몰린 폭증 분포에서도 hash semi join으로 테이블을 한 번씩만 읽습니다
-- (db-hotpath 계획 B4). 186이 만든 인덱스를 제거합니다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_source_collection_checkpoints_updated_identity;
