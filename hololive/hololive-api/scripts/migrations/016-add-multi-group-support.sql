-- 016-add-multi-group-support.sql
-- 멀티 그룹 지원을 위한 컬럼 추가 및 개인세 VTuber 시드 데이터 삽입
-- 주요 변경사항:
--   1. members 테이블에 org, suborg, sync_source 컬럼 추가
--   2. 기존 데이터 Hololive로 백필 및 NOT NULL 제약 추가
--   3. 인덱스 생성 (org, org+english_name 복합)
--   4. 개인세 VTuber 데이터 삽입 (결城さくな, 사메코 사바)

-- Step 1: 컬럼 추가 (nullable로 시작하여 락 최소화)
ALTER TABLE members ADD COLUMN IF NOT EXISTS org VARCHAR(50);
ALTER TABLE members ADD COLUMN IF NOT EXISTS suborg VARCHAR(100);
ALTER TABLE members ADD COLUMN IF NOT EXISTS sync_source VARCHAR(20);

-- Step 2: 기존 데이터 백필 (별도 트랜잭션으로 처리)
UPDATE members SET org = 'Hololive', sync_source = 'holodex' WHERE org IS NULL;

-- Step 3: NOT NULL 제약 추가 (데이터가 채워진 상태이므로 안전)
ALTER TABLE members ALTER COLUMN org SET NOT NULL;
ALTER TABLE members ALTER COLUMN sync_source SET NOT NULL;

-- Step 4: 인덱스 생성 (조회 성능 최적화)
CREATE INDEX IF NOT EXISTS idx_members_org ON members(org);
CREATE INDEX IF NOT EXISTS idx_members_org_english_name ON members(org, english_name);

-- Step 5: 개인세 VTuber 멤버 삽입 (sync_source='manual')
-- 대상 없는 ON CONFLICT DO NOTHING은 slug UNIQUE가 008에서 사라진 뒤 아무것도 막지 못했다
-- (id는 매번 새로 채번되어 충돌 불발) — 064/068과 같은 NOT EXISTS 가드로 멱등을 보장한다.
INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, org, sync_source, status, is_graduated, aliases)
SELECT v.slug, v.channel_id, v.english_name, v.japanese_name, v.korean_name, v.org, v.sync_source, v.status, v.is_graduated, v.aliases::jsonb
FROM (VALUES
  ('yuuki-sakuna', 'UCrV1Hf5r8P148idjoSfrGEQ', 'Yuuki Sakuna', '結城さくな', '유우키 사쿠나', 'Indie', 'manual', 'active', false, '{"ko":["사쿠나","사쿠탄"],"ja":["さくな","さくたん"]}'),
  ('sameko-saba', 'UCxsZ6NCzjU_t4YSxQLBcM5A', 'Sameko Saba', '鮫子サバ', '사메코 사바', 'Indie', 'manual', 'active', false, '{"ko":["사바","사메코"],"ja":["サバ","鮫子"]}')
) AS v(slug, channel_id, english_name, japanese_name, korean_name, org, sync_source, status, is_graduated, aliases)
WHERE NOT EXISTS (
  SELECT 1 FROM members m WHERE m.channel_id = v.channel_id OR m.slug = v.slug
);

COMMENT ON COLUMN members.org IS '소속 조직 (Hololive, Indie 등)';
COMMENT ON COLUMN members.suborg IS '하위 조직 또는 그룹 (Gen1, EN, JP 등)';
COMMENT ON COLUMN members.sync_source IS '데이터 동기화 소스 (holodex, manual 등)';
