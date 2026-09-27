-- 177/189가 만든 legacy_collector 진단 backfill 트리거와 함수를 지운다. 이 트리거는 typed last_failure_*를 쓰지 않던
-- 이전 collector 이미지와 섞여 돌 때만 필요했고, 현재 collector는 release transaction에서 그 덮어쓰기를 되돌려야 했다.
-- 2026-09-26 T18에서 모든 collector가 2026-09-25 release이고 legacy_collector 최종 실패가 2026-08-16이며 트리거 이전
-- collector의 rollback 이미지가 없음을 확인했다(PLN-20260926-stack-audit-refactoring T17). 기존 행의 legacy_collector 값은
-- 이력으로 남긴다. 번호는 운영 적용된 live-evidence 218~220 다음이며, restore 우회를 지운 collector 배포보다 먼저 중앙 db-migrate로 적용한다.
DROP TRIGGER IF EXISTS youtube_collection_job_lease_failure_diagnostics_backfill ON youtube_collection_job_leases;

DROP FUNCTION IF EXISTS populate_youtube_collection_job_lease_failure_diagnostics();
