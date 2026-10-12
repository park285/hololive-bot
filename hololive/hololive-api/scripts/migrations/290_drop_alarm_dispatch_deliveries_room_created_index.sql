-- delivery를 room_id로 거르는 조회는 관리 목록뿐이다. 이 목록은 상태 조건과 함께 d.id 역순으로 읽으므로 상태 인덱스가
-- 범위를 정하고, (room_id, created_at) 순서는 쓰이지 않는다. 2026-10-11 운영 조회: 2026-08-23 이후 idx_scan 0.
DROP INDEX CONCURRENTLY IF EXISTS public.idx_alarm_dispatch_deliveries_room_created;
