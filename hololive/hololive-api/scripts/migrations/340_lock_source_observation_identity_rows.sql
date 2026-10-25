-- lock_source_observation_identity는 uq_source_observation_identity 전체 키로 조회하므로 최대 1행을 돌려준다.
-- SETOF 함수의 기본 행 추정(1000)이 남아 있으면 publish set의 existing CTE가 입력 100행 × 1000으로 과대 추정되고,
-- 그 뒤 checkpoint·checkpoint_write 조인이 표 전체 seq scan + 다중 batch hash join으로 풀려 발행마다
-- 임시 파일이 생겼다(2026-10-24 운영 관측: holo-postgres 하루 약 6,800만 줄의 temporary file 로그).
-- ROWS만 바꾸므로 함수 본문·권한·search_path는 317 그대로다.
ALTER FUNCTION public.lock_source_observation_identity(text, text, text, text, smallint, bigint) ROWS 1;
