-- 024-room-based-alarm-lookup.sql
-- 알람 시스템을 room-based PRIMARY 조회로 전환
-- user_id는 감사(audit) 목적으로 유지하되, 조회/삭제 키에서 제외

-- 1. 중복 제거: (room_id, channel_id) 기준으로 최신 1건만 보존
DELETE FROM alarms a
USING (
    SELECT room_id, channel_id, MAX(id) AS keep_id
    FROM alarms
    GROUP BY room_id, channel_id
    HAVING COUNT(id) > 1
) dup
WHERE a.room_id = dup.room_id
  AND a.channel_id = dup.channel_id
  AND a.id != dup.keep_id;

-- 2. 기존 unique constraint 삭제
ALTER TABLE alarms DROP CONSTRAINT IF EXISTS alarms_unique;

-- 3. room_id + channel_id 기준 unique constraint 생성
ALTER TABLE alarms DROP CONSTRAINT IF EXISTS alarms_room_channel_unique;
ALTER TABLE alarms ADD CONSTRAINT alarms_room_channel_unique UNIQUE (room_id, channel_id);

-- 4. 기존 user_id 포함 인덱스 삭제
DROP INDEX IF EXISTS idx_alarms_room_user;
DROP INDEX IF EXISTS idx_alarms_room_user_created;

-- 5. room_id 기준 조회 인덱스 생성
CREATE INDEX IF NOT EXISTS idx_alarms_room_created ON alarms (room_id, created_at);

-- 롤백:
-- ALTER TABLE alarms DROP CONSTRAINT IF EXISTS alarms_room_channel_unique;
-- ALTER TABLE alarms ADD CONSTRAINT alarms_unique UNIQUE (room_id, user_id, channel_id);
-- CREATE INDEX IF NOT EXISTS idx_alarms_room_user ON alarms (room_id, user_id);
-- CREATE INDEX IF NOT EXISTS idx_alarms_room_user_created ON alarms (room_id, user_id, created_at);
-- DROP INDEX IF EXISTS idx_alarms_room_created;
