-- 037_acl_blacklist_mode.sql
-- ACL 서비스에 블랙리스트/화이트리스트 토글 기능 추가
-- 1. acl_rooms에 list_type 컬럼 추가 (기존 데이터는 whitelist로 유지)
-- 2. composite unique index로 변경 (같은 room_id가 whitelist/blacklist 각각 존재 가능)
-- 3. 블랙리스트 모드 설정 시드
-- 4. 블랙리스트 룸 시드

-- ============================================================================
-- 1. list_type 컬럼 추가 (없으면)
-- ============================================================================
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'acl_rooms'
          AND column_name = 'list_type'
    ) THEN
        ALTER TABLE acl_rooms ADD COLUMN list_type VARCHAR(16) NOT NULL DEFAULT 'whitelist';
    END IF;
END $$;

-- ============================================================================
-- 2. unique index 변경: room_id → (room_id, list_type)
-- ============================================================================
-- 기존 room_id 단독 unique constraint 제거 (PostgreSQL은 constraint가 있으면 DROP INDEX 불가)
ALTER TABLE acl_rooms DROP CONSTRAINT IF EXISTS acl_rooms_room_id_key;
DROP INDEX IF EXISTS acl_rooms_room_id_key;
DROP INDEX IF EXISTS idx_room_list;

-- 새 composite unique index 생성
CREATE UNIQUE INDEX IF NOT EXISTS idx_room_list ON acl_rooms (room_id, list_type);

-- ============================================================================
-- 3. 블랙리스트 모드 설정 시드
-- ============================================================================
INSERT INTO acl_settings (key, value)
VALUES ('mode', 'blacklist')
ON CONFLICT (key) DO UPDATE SET value = 'blacklist';

-- ACL 활성화 (블랙리스트 모드가 동작하려면 enabled=true 필수)
INSERT INTO acl_settings (key, value)
VALUES ('enabled', 'true')
ON CONFLICT (key) DO UPDATE SET value = 'true';

-- ============================================================================
-- 4. 블랙리스트 룸 시드
-- ============================================================================
INSERT INTO acl_rooms (room_id, list_type)
VALUES ('18219201472247343', 'blacklist')
ON CONFLICT (room_id, list_type) DO NOTHING;
