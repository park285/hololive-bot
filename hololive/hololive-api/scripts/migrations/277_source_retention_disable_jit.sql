-- 2026-10-07 서울 운영 pg_stat_statements(2026-08-23 이후 누적): 두 retention 함수의 내부 문장에서 JIT가 실행 시간의
-- 61%(observation, 22,252회 10,838초/17,679초)와 77%(application, 28,918회 4,766초/6,206초)를 차지했고, 같은 날
-- 7분 구간에서는 85~96%였다. generate_subscripts 행 추정(1000)에 정책별 LIMIT 1000이 곱해져 후보 추정이 백만 행이 되고,
-- 그 추정 비용이 JIT inlining·optimization 기준을 넘기 때문이다. 후보 SELECT의 EXPLAIN ANALYZE는 JIT on 207~220 ms,
-- off 3.2~3.5 ms였다. JIT는 실행 방식만 바꾸므로 결과·잠금·보호 조건은 같다. 함수 본문과 권한은 그대로 두고 함수
-- 설정에만 jit = off를 더한다. 이미 SECURITY DEFINER라 인라인 대상이 아니므로 SET 절이 인라인 여부를 바꾸지 않는다.
-- 같은 값의 재설정은 멱등이다. 되돌릴 때는 새 migration에서 RESET jit를 쓴다.
-- 두 함수를 CREATE OR REPLACE로 다시 정의하면 이 설정이 사라지므로 새 정의에도 SET jit = off를 넣어야 한다.
ALTER FUNCTION public.delete_source_observation_retention_batch(TEXT[], TIMESTAMPTZ[], INTEGER) SET jit = off;
ALTER FUNCTION public.delete_source_observation_application_retention_batch(TEXT[], TIMESTAMPTZ[], INTEGER) SET jit = off;
