-- 247이 245의 lifecycle_origin 백필용으로 만든 부분 인덱스다. 백필 프로시저는 245가 실행 뒤 지웠고, 런타임 조회는
-- observation_kind 조건이 없어 이 인덱스의 부분 술어를 만족하지 못하므로 쓸 수 없다. 술어에 맞는 application INSERT마다
-- 갱신 비용만 남는다. 2026-10-08 서울 운영 조회: 약 10 MB, 통계 초기화 이력 없이 누적 idx_scan 5813이며
-- last_idx_scan 2026-10-06 07:35:59 UTC 이후 사용 기록이 없다. 245 백필(2026-09-30 적용) 뒤의 그 스캔 주체는 확인하지
-- 못했다. 코드에는 이 술어를 쓰는 조회가 없으므로 수동·진단 조회로 추정한다.
-- 기존 ledger가 있는 DB에서는 일반 DROP INDEX가 거부되므로 CONCURRENTLY 단일 문장으로 지운다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_source_application_live_origin;
