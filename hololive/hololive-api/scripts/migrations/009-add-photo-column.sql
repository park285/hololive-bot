-- 009-add-photo-column.sql
-- 멤버 테이블에 프로필 이미지 URL 컬럼 추가
-- Holodex API에서 주기적으로 동기화하여 API 호출을 최소화

-- photo 컬럼 추가 (YouTube 프로필 이미지 URL, 최대 1024x1024 고화질)
ALTER TABLE members ADD COLUMN IF NOT EXISTS photo TEXT;

-- photo_updated_at 컬럼 추가 (마지막 동기화 시간 추적용)
ALTER TABLE members ADD COLUMN IF NOT EXISTS photo_updated_at TIMESTAMPTZ;

-- 인덱스 추가 (photo_updated_at으로 오래된 레코드 조회 시 사용)
CREATE INDEX IF NOT EXISTS idx_members_photo_updated_at ON members (photo_updated_at);

COMMENT ON COLUMN members.photo IS 'YouTube 채널 프로필 이미지 URL (Holodex에서 동기화, 고화질 =s1024)';
COMMENT ON COLUMN members.photo_updated_at IS 'photo 컬럼 마지막 동기화 시간';
