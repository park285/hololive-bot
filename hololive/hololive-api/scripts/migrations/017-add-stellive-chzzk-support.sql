-- 017-add-stellive-chzzk-support.sql
-- 스텔라이브 지원 및 치지직 채널 ID 컬럼 추가
-- 주요 변경사항:
--   1. members 테이블에 chzzk_channel_id 컬럼 추가
--   2. 스텔라이브 VTuber 8명 데이터 삽입 (치지직 채널 ID 포함)

BEGIN;

-- Step 1: chzzk_channel_id 컬럼 추가 (nullable)
ALTER TABLE members ADD COLUMN IF NOT EXISTS chzzk_channel_id VARCHAR(32);

-- Step 2: 스텔라이브 멤버 삽입
INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, org, sync_source, status, is_graduated, aliases, chzzk_channel_id)
VALUES 
  ('airi-kanna', 'UC6YnTqZidFg4WUiXpiCtSSQ', 'Airi Kanna', '藍里かんな', '아이리 칸나', 'Stellive', 'holodex', 'graduated', true, '{"ko":["칸나","대장용","락용","간나"],"ja":["藍里","かんな"]}', '82136e09328ffc9143924707293a566d'),
  ('ayatsuno-yuni', 'UClbYIn9LDbbFZ9w2shX3K0g', 'Ayatsuno Yuni', '純角ユニ', '아야츠노 유니', 'Stellive', 'holodex', 'active', false, '{"ko":["유니","유니링","유니찌","정윤희"],"ja":["純角","ユニ"]}', 'f997979606554ef4827038e244845582'),
  ('arahashi-tabi', 'UCq-U-D8O6_6e4X6r-z9V0w', 'Arahashi Tabi', '荒橋タビ', '아라하시 타비', 'Stellive', 'holodex', 'active', false, '{"ko":["타비","뿡댕이","댕댕이","닌자타비"],"ja":["荒橋","タビ"]}', '264b3c95982881a7b6cf09e46a6f1d17'),
  ('shirayuki-hina', 'UC99CUC6yR6O_uXyS_3K7yKA', 'Shirayuki Hina', '白雪ひな', '시라유키 히나', 'Stellive', 'holodex', 'active', false, '{"ko":["히나","히나피","공주","흰눈곰","존 히나"],"ja":["白雪","ひな"]}', '464971337583f6055bc5eb31e42b2600'),
  ('neneko-mashiro', 'UC9o9D7U5O8V0A-zO0v7UeLw', 'Neneko Mashiro', '音々子ましろ', '네네코 마시로', 'Stellive', 'holodex', 'active', false, '{"ko":["마시로","시로","밍대장","밍"],"ja":["音々子","ましろ"]}', '6575122f3ca56822f3068e1a5f6e8979'),
  ('akane-lize', 'UC9m5xP6u69zXpD7MscY-uYQ', 'Akane Lize', '朱音リゼ', '아카네 리제', 'Stellive', 'holodex', 'active', false, '{"ko":["리제","리제황녀","피엔나","저챗퀸","천마"],"ja":["朱音","リゼ"]}', '0013898687707470f1a547781b046043'),
  ('tenko-shibuki', 'UCYxLMfeX1CbMBll9MsGlzmw', 'Tenko Shibuki', '天鼓紫吹', '텐코 시부키', 'Stellive', 'holodex', 'active', false, '{"ko":["시부키","부키","북대장","땡코 시부키"],"ja":["天鼓","紫吹"]}', '0009623253b7c4d51965f7c3554e2f9d'),
  ('hanako-nana', 'UCcA21_PzN1EhNe7xS4MJGsQ', 'Hanako Nana', '華子ナナ', '하나코 나나', 'Stellive', 'holodex', 'active', false, '{"ko":["나나","나교수님","77년생","쌍칠아재","굴리트"],"ja":["華子","ナナ"]}', 'd0b98f2192780362c12b7754d92911b6')
ON CONFLICT DO NOTHING;

COMMIT;

COMMENT ON COLUMN members.chzzk_channel_id IS '치지직 채널 ID (Stellive 등 한국 VTuber용)';
