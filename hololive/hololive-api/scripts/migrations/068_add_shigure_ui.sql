-- 068_add_shigure_ui.sql
-- 시구레 우이(しぐれうい / 우이마마) 채널 등록
-- 주요 내용:
--   1. 시구레 우이를 members에 추가 (채널 1개 = 1행)
--
-- 설계 메모:
--   - 시구레 우이는 실제 소속상 개인세(Independents)이나, 다수 홀로멤 캐릭터 디자인을
--     담당한 "우이마마"로서 홀로멤으로 취급하는 밈을 반영해 org='Hololive'로 등록한다.
--   - 공식 홀로라이브 프로필(유닛)이 없어 멤버 디렉토리에서는 org 폴백으로 "기타"에 분류된다.
--   - Holodex org sync 대상이 아니므로 sync_source='manual'로 표시해 수동 관리 대상임을 명시한다.
--   - channel_id 기준 NOT EXISTS 가드로 멱등 INSERT (재적용 안전).

BEGIN;

INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name, short_korean_name, org, sync_source, status, is_graduated, aliases)
SELECT v.slug, v.channel_id, v.english_name, v.japanese_name, v.korean_name, v.short_korean_name, v.org, v.sync_source, v.status, v.is_graduated, v.aliases::jsonb
FROM (VALUES
  ('shigure-ui', 'UCt30jJgChL8qeT9VPadidSw', 'Shigure Ui', 'しぐれうい', '시구레 우이', '우이', 'Hololive', 'manual', 'active', false,
    '{"ko":["시구레우이","시구레 우이","우이","우이마마"],"ja":["しぐれうい","ういママ","うい"]}')
) AS v(slug, channel_id, english_name, japanese_name, korean_name, short_korean_name, org, sync_source, status, is_graduated, aliases)
WHERE NOT EXISTS (
  SELECT 1 FROM members m WHERE m.channel_id = v.channel_id
);

COMMIT;
