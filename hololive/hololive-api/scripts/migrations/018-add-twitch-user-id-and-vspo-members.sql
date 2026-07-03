-- 018-add-twitch-user-id-and-vspo-members.sql
-- Twitch User ID 컬럼 추가 및 VSPO 멤버 삽입
-- 주요 변경사항:
--   1. members 테이블에 twitch_user_id 컬럼 추가
--   2. 기존 Hololive 멤버의 Twitch ID 업데이트
--   3. VSPO 멤버 5명 삽입 (Twitch 활성 채널 보유 멤버)

BEGIN;

-- Step 1: twitch_user_id 컬럼 추가 (nullable)
ALTER TABLE members ADD COLUMN IF NOT EXISTS twitch_user_id VARCHAR(50);

-- Step 2: 인덱스 생성 (Twitch ID로 조회 최적화)
CREATE INDEX IF NOT EXISTS idx_members_twitch_user_id ON members(twitch_user_id) WHERE twitch_user_id IS NOT NULL;

-- Step 3: 기존 Hololive 멤버 Twitch ID 업데이트
-- EN Members
UPDATE members SET twitch_user_id = 'ceciliaimmergreen_holo' WHERE english_name = 'Cecilia Immergreen' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'elizabethbloodflame' WHERE english_name = 'Elizabeth Rose Bloodflame' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'fuwamoco_hololive' WHERE english_name = 'FuwaMoco' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'gigimurin_hololive' WHERE english_name = 'Gigi Murin' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'hakosbaelz_hololive' WHERE english_name = 'Hakos Baelz' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'kosekibijou_hololive' WHERE english_name = 'Koseki Bijou' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'moricalliope_hololive' WHERE english_name = 'Mori Calliope' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'nerissaravencroft_hololive' WHERE english_name = 'Nerissa Ravencroft' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'shiorinovella_hololive' WHERE english_name = 'Shiori Novella' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'taaboringirl' WHERE english_name = 'Takanashi Kiara' AND twitch_user_id IS NULL;

-- JP Members
UPDATE members SET twitch_user_id = 'hakuikoyori_hololive' WHERE english_name = 'Hakui Koyori' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'kazamairoha_holo' WHERE english_name = 'Kazama Iroha' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'laplusdarknesss_hololive' WHERE english_name = 'La+ Darknesss' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'robocosan_hololive' WHERE english_name = 'Roboco-san' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'sakuramiko_hololive' WHERE english_name = 'Sakura Miko' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'shishirobotan_hololive' WHERE english_name = 'Shishiro Botan' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'tokoyamitowa_holo' WHERE english_name = 'Tokoyami Towa' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'usadapekora_hololive' WHERE english_name = 'Usada Pekora' AND twitch_user_id IS NULL;

-- ID Members
UPDATE members SET twitch_user_id = 'kaaboringirl_hololive' WHERE english_name = 'Kaela Kovalskia' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'kobokanaeru_hololive' WHERE english_name = 'Kobo Kanaeru' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'kureijiollie_hololive' WHERE english_name = 'Kureiji Ollie' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'moonahoshinova' WHERE english_name = 'Moona Hoshinova' AND twitch_user_id IS NULL;
UPDATE members SET twitch_user_id = 'vestiazeta_hololive' WHERE english_name = 'Vestia Zeta' AND twitch_user_id IS NULL;

-- Step 4: VSPO 멤버 삽입
-- 대상 없는 ON CONFLICT DO NOTHING은 slug UNIQUE 부재 상태에서 무방비 — NOT EXISTS 가드로 멱등 보장(064/068 관례).
INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, org, sync_source, status, is_graduated, aliases, twitch_user_id)
SELECT v.slug, v.channel_id, v.english_name, v.japanese_name, v.korean_name, v.org, v.sync_source, v.status, v.is_graduated, v.aliases::jsonb, v.twitch_user_id
FROM (VALUES
  ('tachibana-hinano', 'UCvUc0m317LWTTPZoBQV479A', 'Tachibana Hinano', '橘ひなの', '타치바나 히나노', 'VSPO', 'manual', 'active', false, '{"ko":["타치바나 히나노","히나노","히나땅"],"ja":["橘ひなの","ひなの"]}', 'hinanotachiba7'),
  ('ichinose-uruha', 'UC5LyYg6cCA4yHEYvtUsir3g', 'Ichinose Uruha', '一ノ瀬うるは', '이치노세 우루하', 'VSPO', 'manual', 'active', false, '{"ko":["이치노세 우루하","우루하"],"ja":["一ノ瀬うるは","うるは"]}', 'uruhaichinose'),
  ('kaga-nazuna', 'UCiMG6VdScBabPhJ1ZtaVmbw', 'Kaga Nazuna', '花芽なずな', '카가 나즈나', 'VSPO', 'manual', 'active', false, '{"ko":["카가 나즈나","나즈나"],"ja":["花芽なずな","なずな"]}', 'nazunakaga'),
  ('kaminari-qpi', 'UCMp55EbT_ZlqiMS3lCj01BQ', 'Kaminari Qpi', '神成きゅぴ', '카미나리 큐피', 'VSPO', 'manual', 'active', false, '{"ko":["카미나리 큐피","큐피"],"ja":["神成きゅぴ","きゅぴ"]}', 'kaminariqpi'),
  ('yakumo-beni', 'UCjXBuHmWkieBApgBhDuJMMQ', 'Yakumo Beni', '八雲べに', '야쿠모 베니', 'VSPO', 'manual', 'active', false, '{"ko":["야쿠모 베니","베니"],"ja":["八雲べに","べに"]}', 'yakumobeni')
) AS v(slug, channel_id, english_name, japanese_name, korean_name, org, sync_source, status, is_graduated, aliases, twitch_user_id)
WHERE NOT EXISTS (
  SELECT 1 FROM members m WHERE m.channel_id = v.channel_id OR m.slug = v.slug
);

COMMIT;

COMMENT ON COLUMN members.twitch_user_id IS 'Twitch user_login (소문자 username, immutable)';
