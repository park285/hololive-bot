-- youtube_community_shorts_alarm_states는 PK와 idx_ycsas_kind_content로만 찾고, delivery_status는 갱신 대상일 뿐
-- 조회 조건이 아니다. 2026-10-11 운영 조회: idx_scan 4(마지막 2026-09-01). 같은 기간 런타임 role이 이 테이블에
-- 수십만 번 접근했으므로 이 4회는 수동 진단 조회로 본다.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_ycsas_delivery_status;
