-- 공식 소개문 대신 소속 유닛과 기본 링크를 저장한다. 기존 멤버 ID/채널/상태는 보존한다.
BEGIN;
ALTER TABLE members ADD COLUMN IF NOT EXISTS units TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE members ADD COLUMN IF NOT EXISTS official_link TEXT;
COMMENT ON COLUMN members.units IS '기수·유닛 표시명. 복수 소속을 보존하며 빈 배열은 미등록이다.';
COMMENT ON COLUMN members.official_link IS '확인된 공식 멤버 페이지 URL. 없으면 NULL이다.';

-- 병행 등록으로 같은 slug가 다른 identity에 연결되었으면 적용을 중단한다.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM members
        WHERE slug IN ('holoan-room', 'izuki-michiru', 'hanazono-sayaka', 'kazeshiro-yuki')
            AND (org, channel_id) IS DISTINCT FROM ('Hololive', 'UCozx5csNhCx1wsVq3SZVkBQ')
    ) THEN
        RAISE EXCEPTION 'holoAN member identity conflict';
    END IF;
END;
$$;

-- fresh bootstrap에서도 공용 채널 대표를 개인보다 먼저 등록한다. 기존 대표는 유지한다.
INSERT INTO members (slug, channel_id, english_name, korean_name, short_korean_name, org, sync_source, aliases)
VALUES ('holoan-room', 'UCozx5csNhCx1wsVq3SZVkBQ', 'holoAN', '홀로아나', '홀로아나', 'Hololive', 'manual', '{"ko":["홀로아나"],"ja":["ホロアナ"]}')
ON CONFLICT (slug) DO NOTHING;

-- 공식 명단의 holoAN 개인 3명. 모두 기존 공용 채널을 사용하며 생일은 미공개이다.
-- 출처: https://hololive.hololivepro.com/en/talents/{slug}/ (2026-09-13 확인)
INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name,
    short_korean_name, status, is_graduated, aliases, org, sync_source, units, official_link, debut_date)
VALUES
    ('izuki-michiru', 'UCozx5csNhCx1wsVq3SZVkBQ', 'Izuki Michiru', '井月みちる', '이즈키 미치루', '미치루', 'active', false,
     '{"ko":["이즈키 미치루","미치루"],"ja":["井月みちる","みちる"]}', 'Hololive', 'manual', ARRAY['holoAN'],
     'https://hololive.hololivepro.com/talents/izuki-michiru/', DATE '2025-10-15'),
    ('hanazono-sayaka', 'UCozx5csNhCx1wsVq3SZVkBQ', 'Hanazono Sayaka', '花園さやか', '하나조노 사야카', '사야카', 'active', false,
     '{"ko":["하나조노 사야카","사야카"],"ja":["花園さやか","さやか"]}', 'Hololive', 'manual', ARRAY['holoAN'],
     'https://hololive.hololivepro.com/talents/hanazono-sayaka/', DATE '2025-11-10'),
    ('kazeshiro-yuki', 'UCozx5csNhCx1wsVq3SZVkBQ', 'Kazeshiro Yuki', '風白ゆき', '카제시로 유키', '유키', 'active', false,
     '{"ko":["카제시로 유키","유키"],"ja":["風白ゆき","ゆき"]}', 'Hololive', 'manual', ARRAY['holoAN'],
     'https://hololive.hololivepro.com/talents/kazeshiro-yuki/', DATE '2025-12-30')
ON CONFLICT (slug) DO NOTHING;

-- 기존 기수 정보를 이관한다. 신규 DB에서 아직 등록되지 않은 멤버는 만들지 않는다.
WITH seed(slug, units, official_link) AS (VALUES
    ('airani-iofifteen', ARRAY['AREA15']::text[], 'https://hololive.hololivepro.com/talents/airani-iofifteen/'),
    ('akai-haato', ARRAY['홀로라이브 1기생']::text[], 'https://hololive.hololivepro.com/talents/akai-haato/'),
    ('aki-rosenthal', ARRAY['홀로라이브 1기생']::text[], 'https://hololive.hololivepro.com/talents/aki-rosenthal/'),
    ('amane-kanata', ARRAY['홀로라이브 4기생']::text[], 'https://hololive.hololivepro.com/talents/amane-kanata/'),
    ('anya-melfissa', ARRAY['holoro']::text[], 'https://hololive.hololivepro.com/talents/anya-melfissa/'),
    ('ayunda-risu', ARRAY['AREA15']::text[], 'https://hololive.hololivepro.com/talents/ayunda-risu/'),
    ('azki', ARRAY['홀로라이브 0기생']::text[], 'https://hololive.hololivepro.com/talents/azki/'),
    ('cecilia-immergreen', ARRAY['Justice']::text[], 'https://hololive.hololivepro.com/talents/cecilia-immergreen/'),
    ('ceres-fauna', ARRAY['Promise']::text[], 'https://hololive.hololivepro.com/talents/ceres-fauna/'),
    ('elizabeth-rose-bloodflame', ARRAY['Justice']::text[], 'https://hololive.hololivepro.com/talents/elizabeth-rose-bloodflame/'),
    ('fuwawa-abyssgard', ARRAY['Advent']::text[], 'https://hololive.hololivepro.com/talents/fuwawa-abyssgard/'),
    ('gawr-gura', ARRAY['Myth']::text[], 'https://hololive.hololivepro.com/talents/gawr-gura/'),
    ('gigi-murin', ARRAY['Justice']::text[], 'https://hololive.hololivepro.com/talents/gigi-murin/'),
    ('hakos-baelz', ARRAY['Promise']::text[], 'https://hololive.hololivepro.com/talents/hakos-baelz/'),
    ('hakui-koyori', ARRAY['비밀결사 holoX']::text[], 'https://hololive.hololivepro.com/talents/hakui-koyori/'),
    ('himemori-luna', ARRAY['홀로라이브 4기생']::text[], 'https://hololive.hololivepro.com/talents/himemori-luna/'),
    ('hiodoshi-ao', ARRAY['ReGLOSS']::text[], 'https://hololive.hololivepro.com/talents/hiodoshi-ao/'),
    ('hoshimachi-suisei', ARRAY['홀로라이브 0기생']::text[], 'https://hololive.hololivepro.com/talents/hoshimachi-suisei/'),
    ('houshou-marine', ARRAY['홀로라이브 3기생']::text[], 'https://hololive.hololivepro.com/talents/houshou-marine/'),
    ('ichijou-ririka', ARRAY['ReGLOSS']::text[], 'https://hololive.hololivepro.com/talents/ichijou-ririka/'),
    ('inugami-korone', ARRAY['홀로라이브 게이머즈']::text[], 'https://hololive.hololivepro.com/talents/inugami-korone/'),
    ('irys', ARRAY['Promise']::text[], 'https://hololive.hololivepro.com/talents/irys/'),
    ('isaki-riona', ARRAY['FLOW GLOW']::text[], 'https://hololive.hololivepro.com/talents/isaki-riona/'),
    ('juufuutei-raden', ARRAY['ReGLOSS']::text[], 'https://hololive.hololivepro.com/talents/juufuutei-raden/'),
    ('kaela-kovalskia', ARRAY['holoh3ro']::text[], 'https://hololive.hololivepro.com/talents/kaela-kovalskia/'),
    ('kazama-iroha', ARRAY['비밀결사 holoX']::text[], 'https://hololive.hololivepro.com/talents/kazama-iroha/'),
    ('kikirara-vivi', ARRAY['FLOW GLOW']::text[], 'https://hololive.hololivepro.com/talents/kikirara-vivi/'),
    ('kiryu-coco', ARRAY['홀로라이브 4기생']::text[], 'https://hololive.hololivepro.com/talents/kiryu-coco/'),
    ('kobo-kanaeru', ARRAY['holoh3ro']::text[], 'https://hololive.hololivepro.com/talents/kobo-kanaeru/'),
    ('koganei-niko', ARRAY['FLOW GLOW']::text[], 'https://hololive.hololivepro.com/talents/koganei-niko/'),
    ('koseki-bijou', ARRAY['Advent']::text[], 'https://hololive.hololivepro.com/talents/koseki-bijou/'),
    ('kureiji-ollie', ARRAY['holoro']::text[], 'https://hololive.hololivepro.com/talents/kureiji-ollie/'),
    ('la-darknesss', ARRAY['비밀결사 holoX']::text[], 'https://hololive.hololivepro.com/talents/la-darknesss/'),
    ('minato-aqua', ARRAY['홀로라이브 2기생']::text[], 'https://hololive.hololivepro.com/talents/minato-aqua/'),
    ('mizumiya-su', ARRAY['FLOW GLOW']::text[], 'https://hololive.hololivepro.com/talents/mizumiya-su/'),
    ('mococo-abyssgard', ARRAY['Advent']::text[], 'https://hololive.hololivepro.com/talents/mococo-abyssgard/'),
    ('momosuzu-nene', ARRAY['홀로라이브 5기생']::text[], 'https://hololive.hololivepro.com/talents/momosuzu-nene/'),
    ('moona-hoshinova', ARRAY['AREA15']::text[], 'https://hololive.hololivepro.com/talents/moona-hoshinova/'),
    ('mori-calliope', ARRAY['Myth']::text[], 'https://hololive.hololivepro.com/talents/mori-calliope/'),
    ('murasaki-shion', ARRAY['홀로라이브 2기생']::text[], 'https://hololive.hololivepro.com/talents/murasaki-shion/'),
    ('nakiri-ayame', ARRAY['홀로라이브 2기생']::text[], 'https://hololive.hololivepro.com/talents/nakiri-ayame/'),
    ('nanashi-mumei', ARRAY['Promise']::text[], 'https://hololive.hololivepro.com/talents/nanashi-mumei/'),
    ('natsuiro-matsuri', ARRAY['홀로라이브 1기생']::text[], 'https://hololive.hololivepro.com/talents/natsuiro-matsuri/'),
    ('nekomata-okayu', ARRAY['홀로라이브 게이머즈']::text[], 'https://hololive.hololivepro.com/talents/nekomata-okayu/'),
    ('nerissa-ravencroft', ARRAY['Advent']::text[], 'https://hololive.hololivepro.com/talents/nerissa-ravencroft/'),
    ('ninomae-inanis', ARRAY['Myth']::text[], 'https://hololive.hololivepro.com/talents/ninomae-inanis/'),
    ('omaru-polka', ARRAY['홀로라이브 5기생']::text[], 'https://hololive.hololivepro.com/talents/omaru-polka/'),
    ('ookami-mio', ARRAY['홀로라이브 게이머즈']::text[], 'https://hololive.hololivepro.com/talents/ookami-mio/'),
    ('oozora-subaru', ARRAY['홀로라이브 2기생']::text[], 'https://hololive.hololivepro.com/talents/oozora-subaru/'),
    ('otonose-kanade', ARRAY['ReGLOSS']::text[], 'https://hololive.hololivepro.com/talents/otonose-kanade/'),
    ('ouro-kronii', ARRAY['Promise']::text[], 'https://hololive.hololivepro.com/talents/ouro-kronii/'),
    ('pavolia-reine', ARRAY['holoro']::text[], 'https://hololive.hololivepro.com/talents/pavolia-reine/'),
    ('raora-panthera', ARRAY['Justice']::text[], 'https://hololive.hololivepro.com/talents/raora-panthera/'),
    ('rindo-chihaya', ARRAY['FLOW GLOW']::text[], 'https://hololive.hololivepro.com/talents/rindo-chihaya/'),
    ('roboco-san', ARRAY['홀로라이브 0기생']::text[], 'https://hololive.hololivepro.com/talents/roboco-san/'),
    ('sakamata-chloe', ARRAY['홀로X 비밀결사']::text[], 'https://hololive.hololivepro.com/talents/sakamata-chloe/'),
    ('sakuramiko', ARRAY['홀로라이브 0기생']::text[], 'https://hololive.hololivepro.com/talents/sakuramiko/'),
    ('shiori-novella', ARRAY['Advent']::text[], 'https://hololive.hololivepro.com/talents/shiori-novella/'),
    ('shirakami-fubuki', ARRAY['홀로라이브 1기생','홀로라이브 게이머즈']::text[], 'https://hololive.hololivepro.com/talents/shirakami-fubuki/'),
    ('shiranui-flare', ARRAY['홀로라이브 3기생']::text[], 'https://hololive.hololivepro.com/talents/shiranui-flare/'),
    ('shirogane-noel', ARRAY['홀로라이브 3기생']::text[], 'https://hololive.hololivepro.com/talents/shirogane-noel/'),
    ('shishiro-botan', ARRAY['홀로라이브 5기생']::text[], 'https://hololive.hololivepro.com/talents/shishiro-botan/'),
    ('takanashi-kiara', ARRAY['Myth']::text[], 'https://hololive.hololivepro.com/talents/takanashi-kiara/'),
    ('takane-lui', ARRAY['비밀결사 holoX']::text[], 'https://hololive.hololivepro.com/talents/takane-lui/'),
    ('todoroki-hajime', ARRAY['ReGLOSS']::text[], 'https://hololive.hololivepro.com/talents/todoroki-hajime/'),
    ('tokino-sora', ARRAY['홀로라이브 0기생']::text[], 'https://hololive.hololivepro.com/talents/tokino-sora/'),
    ('tokoyami-towa', ARRAY['홀로라이브 4기생']::text[], 'https://hololive.hololivepro.com/talents/tokoyami-towa/'),
    ('tsukumo-sana', ARRAY['Council']::text[], 'https://hololive.hololivepro.com/talents/tsukumo-sana/'),
    ('tsunomaki-watame', ARRAY['홀로라이브 4기생']::text[], 'https://hololive.hololivepro.com/talents/tsunomaki-watame/'),
    ('usada-pekora', ARRAY['홀로라이브 3기생']::text[], 'https://hololive.hololivepro.com/talents/usada-pekora/'),
    ('vestia-zeta', ARRAY['holoh3ro']::text[], 'https://hololive.hololivepro.com/talents/vestia-zeta/'),
    ('watson-amelia', ARRAY['Myth']::text[], 'https://hololive.hololivepro.com/talents/watson-amelia/'),
    ('yukihana-lamy', ARRAY['홀로라이브 5기생']::text[], 'https://hololive.hololivepro.com/talents/yukihana-lamy/'),
    ('yuzuki-choco', ARRAY['홀로라이브 2기생']::text[], 'https://hololive.hololivepro.com/talents/yuzuki-choco/')
)
UPDATE members m SET units = s.units, official_link = COALESCE(NULLIF(m.official_link, ''), s.official_link)
FROM seed s WHERE m.slug = s.slug AND m.org = 'Hololive'
    AND (m.units, m.official_link) IS DISTINCT FROM (s.units, COALESCE(NULLIF(m.official_link, ''), s.official_link));
-- 2026-09-13~14 공식 페이지 /talents/{slug}/의 誕生日·初配信日·デビュー日·デビュー를 대조했다.
-- 생일은 공개된 월·일만 보관하므로 윤년 2000을 기준 연도로 쓴다(실제 출생 연도가 아님).
-- 기존 비NULL 날짜는 유지하며, 공식 페이지에 없는 날짜는 추정하지 않는다.
WITH verified_dates(slug, birthday, debut_date) AS (VALUES
    ('airani-iofifteen', DATE '2000-07-15', DATE '2020-04-12'),
    ('akai-haato', DATE '2000-08-10', DATE '2018-06-02'),
    ('aki-rosenthal', DATE '2000-02-17', DATE '2018-06-01'),
    ('amane-kanata', DATE '2000-04-22', DATE '2019-12-27'),
    ('anya-melfissa', DATE '2000-03-12', DATE '2020-12-05'),
    ('ayunda-risu', DATE '2000-01-15', DATE '2020-04-10'),
    ('azki', DATE '2000-07-01', NULL::date),
    ('cecilia-immergreen', DATE '2000-11-11', DATE '2024-06-23'),
    ('ceres-fauna', DATE '2000-03-21', DATE '2021-08-23'),
    ('elizabeth-rose-bloodflame', DATE '2000-04-25', DATE '2024-06-22'),
    ('fuwawa-abyssgard', DATE '2000-02-01', DATE '2023-07-31'),
    ('gawr-gura', DATE '2000-06-20', DATE '2020-09-13'),
    ('gigi-murin', DATE '2000-10-18', DATE '2024-06-22'),
    ('hakos-baelz', DATE '2000-02-29', DATE '2021-08-23'),
    ('hakui-koyori', DATE '2000-03-15', DATE '2021-11-28'),
    ('himemori-luna', DATE '2000-10-10', DATE '2020-01-04'),
    ('hiodoshi-ao', DATE '2000-02-27', DATE '2023-09-09'),
    ('hoshimachi-suisei', DATE '2000-03-22', DATE '2018-03-22'),
    ('houshou-marine', DATE '2000-07-30', DATE '2019-08-11'),
    ('ichijou-ririka', DATE '2000-05-12', DATE '2023-09-09'),
    ('inugami-korone', DATE '2000-10-01', DATE '2019-04-13'),
    ('irys', DATE '2000-03-07', DATE '2021-07-11'),
    ('isaki-riona', DATE '2000-05-29', DATE '2024-11-09'),
    ('juufuutei-raden', DATE '2000-02-04', DATE '2023-09-10'),
    ('kaela-kovalskia', DATE '2000-08-30', DATE '2022-03-26'),
    ('kazama-iroha', DATE '2000-06-18', DATE '2021-11-30'),
    ('kikirara-vivi', DATE '2000-08-27', DATE '2024-11-09'),
    ('kiryu-coco', DATE '2000-06-17', DATE '2019-12-28'),
    ('kobo-kanaeru', DATE '2000-12-12', DATE '2022-03-27'),
    ('koganei-niko', DATE '2000-07-25', DATE '2024-11-09'),
    ('koseki-bijou', DATE '2000-04-14', DATE '2023-07-30'),
    ('kureiji-ollie', DATE '2000-10-13', DATE '2020-12-04'),
    ('la-darknesss', DATE '2000-05-25', DATE '2021-11-26'),
    ('minato-aqua', DATE '2000-12-01', DATE '2018-08-08'),
    ('mizumiya-su', DATE '2000-06-16', DATE '2024-11-09'),
    ('mococo-abyssgard', DATE '2000-02-02', DATE '2023-07-31'),
    ('momosuzu-nene', DATE '2000-03-02', DATE '2020-08-13'),
    ('moona-hoshinova', DATE '2000-02-15', DATE '2020-04-11'),
    ('mori-calliope', DATE '2000-04-04', DATE '2020-09-12'),
    ('murasaki-shion', DATE '2000-12-08', DATE '2018-08-17'),
    ('nakiri-ayame', DATE '2000-12-13', DATE '2018-09-03'),
    ('nanashi-mumei', DATE '2000-08-04', DATE '2021-08-23'),
    ('natsuiro-matsuri', DATE '2000-07-22', DATE '2018-06-01'),
    ('nekomata-okayu', DATE '2000-02-22', DATE '2019-04-06'),
    ('nerissa-ravencroft', DATE '2000-11-21', DATE '2023-07-31'),
    ('ninomae-inanis', DATE '2000-05-20', DATE '2020-09-13'),
    ('omaru-polka', DATE '2000-01-30', DATE '2020-08-16'),
    ('ookami-mio', DATE '2000-08-20', DATE '2018-12-07'),
    ('oozora-subaru', DATE '2000-07-02', DATE '2018-09-16'),
    ('otonose-kanade', DATE '2000-04-20', DATE '2023-09-09'),
    ('ouro-kronii', DATE '2000-03-14', DATE '2021-08-23'),
    ('pavolia-reine', DATE '2000-09-09', DATE '2020-12-06'),
    ('raora-panthera', DATE '2000-05-11', DATE '2024-06-23'),
    ('rindo-chihaya', DATE '2000-07-08', DATE '2024-11-09'),
    ('roboco-san', DATE '2000-05-23', DATE '2018-03-09'),
    ('sakamata-chloe', DATE '2000-05-18', DATE '2021-11-29'),
    ('sakuramiko', DATE '2000-03-05', DATE '2018-08-01'),
    ('shiori-novella', DATE '2000-05-02', DATE '2023-07-30'),
    ('shirakami-fubuki', DATE '2000-10-05', DATE '2018-06-01'),
    ('shiranui-flare', DATE '2000-04-02', DATE '2019-08-07'),
    ('shirogane-noel', DATE '2000-11-24', DATE '2019-08-08'),
    ('shishiro-botan', DATE '2000-09-08', DATE '2020-08-14'),
    ('takanashi-kiara', DATE '2000-07-06', DATE '2020-09-12'),
    ('takane-lui', DATE '2000-06-11', DATE '2021-11-27'),
    ('todoroki-hajime', DATE '2000-06-07', DATE '2023-09-10'),
    ('tokino-sora', DATE '2000-05-15', DATE '2017-09-07'),
    ('tokoyami-towa', DATE '2000-08-08', DATE '2020-01-03'),
    ('tsukumo-sana', DATE '2000-06-10', DATE '2021-08-23'),
    ('tsunomaki-watame', DATE '2000-06-06', DATE '2019-12-29'),
    ('usada-pekora', DATE '2000-01-12', DATE '2019-07-17'),
    ('vestia-zeta', DATE '2000-11-07', DATE '2022-03-25'),
    ('watson-amelia', DATE '2000-01-06', DATE '2020-09-13'),
    ('yukihana-lamy', DATE '2000-11-15', DATE '2020-08-12'),
    ('yuzuki-choco', DATE '2000-02-14', DATE '2018-09-04')
)
UPDATE members m
SET birthday = COALESCE(m.birthday, d.birthday), debut_date = COALESCE(m.debut_date, d.debut_date)
FROM verified_dates d
WHERE m.slug = d.slug AND m.org = 'Hololive'
    AND (m.birthday, m.debut_date) IS DISTINCT FROM (COALESCE(m.birthday, d.birthday), COALESCE(m.debut_date, d.debut_date));
COMMIT;
