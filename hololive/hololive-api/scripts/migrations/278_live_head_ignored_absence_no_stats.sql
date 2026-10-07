-- 2026-10-07 서울 운영 pg_stat_user_tables: youtube_live_reconciliation_heads autoanalyze가 26,174회 평균 384 ms였다
-- (youtube_live_sessions는 43 ms). 같은 날 7분 구간의 1회는 1,440 ms였다. 원소 약 1,579만 개인 ignored_absence_scheduled_for
-- 배열을 ANALYZE가 매번 detoast해 원소 통계를 계산하기 때문이다. 이 열은 SELECT 식과 upsert의 충돌 행 비교에만 쓰이고
-- 계획을 고르는 WHERE·JOIN 술어나 확장 통계에는 없으므로, 통계를 끄면 ANALYZE만 이 열을 건너뛰고 질의 결과와 계획은
-- 같다. 이미 수집된 pg_statistic 행은 해가 없어 그대로 둔다. 같은 값의 재설정은 멱등이다.
-- SET STATISTICS는 SHARE UPDATE EXCLUSIVE 잠금이라 DML은 막지 않지만 실행 중인 (auto)vacuum·analyze를 기다리므로 잠금
-- 대기를 짧게 끊는다. 실패하면 ledger에 남지 않아 다음 실행에서 파일 전체를 다시 적용한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
ALTER TABLE public.youtube_live_reconciliation_heads ALTER COLUMN ignored_absence_scheduled_for SET STATISTICS 0;
COMMIT;
