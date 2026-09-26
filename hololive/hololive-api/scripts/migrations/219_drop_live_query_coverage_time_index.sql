-- LiveQuery는 채널 확인 최신값만 읽는다. 남은 reducer 조회는 채널 GIN·scheduled_for 인덱스를 사용한다.
-- migration 212의 scheduled_for 인덱스와 absence 이력·종료 정책은 유지한다.
DROP INDEX CONCURRENTLY IF EXISTS idx_youtube_live_absence_slots_live_time;
