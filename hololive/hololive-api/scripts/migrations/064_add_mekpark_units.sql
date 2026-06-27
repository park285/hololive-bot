-- 064_add_mekpark_units.sql
-- mekPark(COVER 신규 연습생 육성 프로젝트) 유닛 등록
-- 주요 내용:
--   1. ACHRORA, UNIT B를 각각 "유닛 대분류" 단위로 members에 추가 (채널 1개 = 1행)
--
-- 설계 메모:
--   - mekPark은 홀로라이브 정규 소속이 아닌 별도 육성 프로젝트라 공식 프로필이 없음 → org='mekPark'
--   - pre-debut 단계이나 채널은 활성 상태 → status='active', is_graduated=false
--   - 멤버를 개별로 쪼개지 않고 유닛 채널 1개를 1행으로 처리(채널 공유 케이스는 FUWAMOCO 선례로 channel_id UNIQUE 제약 없음)
--   - channel_id 기준 NOT EXISTS 가드로 멱등 INSERT (재적용 안전)

BEGIN;

INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, org, sync_source, status, is_graduated, aliases)
SELECT v.slug, v.channel_id, v.english_name, v.japanese_name, v.korean_name, v.org, v.sync_source, v.status, v.is_graduated, v.aliases::jsonb
FROM (VALUES
  ('mekpark-achrora', 'UChpRPsAeSZn5DistGacR3iA', 'ACHRORA', 'ACHRORA', '아크로라', 'mekPark', 'manual', 'active', false,
    '{"ko":["아크로라","ACHRORA","멕파크","mekPark"],"ja":["アクロラ","ACHRORA"]}'),
  ('mekpark-unit-b', 'UC3OH5FKQ3qtl4uRme_vZTgA', 'Unit B', 'UNIT B', '유닛 B', 'mekPark', 'manual', 'active', false,
    '{"ko":["유닛비","유닛 B","UNIT B","멕파크","mekPark"],"ja":["UNIT B","宵凪ネオン","玲銘ミラ","清澄ライラ"]}')
) AS v(slug, channel_id, english_name, japanese_name, korean_name, org, sync_source, status, is_graduated, aliases)
WHERE NOT EXISTS (
  SELECT 1 FROM members m WHERE m.channel_id = v.channel_id
);

COMMIT;
