-- アソビ★まわり隊！ 개인 4명과 유닛 채널을 등록한다(2026-09-24 공식 정보 확인).
-- 이름·유닛·데뷔 일정: https://hololive.hololivepro.com/special/22482/
-- 생일: https://hololive.hololivepro.com/talents/{slug}/ 의 DATA.
-- 채널 ID: 위 공식 링크의 YouTube 채널 페이지 externalId.
-- 생일의 2000년은 월·일 저장용 기준 연도이며 실제 출생 연도가 아니다.
-- 공용 채널의 생일·데뷔일은 개인 기념일로 취급하지 않도록 NULL로 둔다.
DO $$
BEGIN
    INSERT INTO members (slug, channel_id, english_name, japanese_name, korean_name,
                         short_korean_name, aliases, org, sync_source, units,
                         official_link, birthday, debut_date)
    VALUES
        ('hyakuto-kyoko', 'UCSjQDxud2HkAO2DVD3lwxmw', 'Hyakuto Kyoko', '百灯キョーコ', '햐쿠토 쿄코',
         '쿄코', '{"ko":["햐쿠토 쿄코","햐쿠토쿄코","쿄코"],"ja":["百灯キョーコ","キョーコ","ひゃくとうきょーこ"]}',
         'Hololive', 'manual', ARRAY['ASOBI★MAWARI-TAI!'],
         'https://hololive.hololivepro.com/talents/hyakuto-kyoko/', DATE '2000-05-08', DATE '2026-09-25'),
        ('achichi-mela', 'UC8eitCE9Z6EwUCs-VUi1blg', 'Achichi Mela', '熱千めら', '아치치 메라',
         '메라', '{"ko":["아치치 메라","아치치메라","메라"],"ja":["熱千めら","めら","あちちめら"]}',
         'Hololive', 'manual', ARRAY['ASOBI★MAWARI-TAI!'],
         'https://hololive.hololivepro.com/talents/achichi-mela/', DATE '2000-04-16', DATE '2026-09-24'),
        ('suzuna-tsuzuri', 'UCy9mgxB8pn2C4aNK_MPthDQ', 'Suzuna Tsuzuri', '鈴鳴つづり', '스즈나 츠즈리',
         '츠즈리', '{"ko":["스즈나 츠즈리","스즈나츠즈리","츠즈리"],"ja":["鈴鳴つづり","つづり","すずなつづり"]}',
         'Hololive', 'manual', ARRAY['ASOBI★MAWARI-TAI!'],
         'https://hololive.hololivepro.com/talents/suzuna-tsuzuri/', DATE '2000-06-28', DATE '2026-09-25'),
        ('sorashina-sopia', 'UCROQtXcp2loQEmvpe5rhJzQ', 'Sorashina Sopia', '宙科そぴあ', '소라시나 소피아',
         '소피아', '{"ko":["소라시나 소피아","소라시나소피아","소피아"],"ja":["宙科そぴあ","そぴあ","そらしなそぴあ"]}',
         'Hololive', 'manual', ARRAY['ASOBI★MAWARI-TAI!'],
         'https://hololive.hololivepro.com/talents/sorashina-sopia/', DATE '2000-06-13', DATE '2026-09-24'),
        ('asobi-mawaritai', 'UCAHwWUotyS3l2qBetFDsjgQ', 'ASOBI★MAWARI-TAI!', 'アソビ★まわり隊！', '아소비★마와리타이!',
         '아소비', '{"ko":["아소비","아소비 마와리타이","아소비마와리타이","아소비★마와리타이!"],"ja":["アソビ★まわり隊！","アソビまわり隊"]}',
         'Hololive', 'manual', ARRAY['ASOBI★MAWARI-TAI!'],
         'https://hololive.hololivepro.com/special/22482/', NULL, NULL)
    ON CONFLICT (slug) DO NOTHING;

    -- 기존 행은 덮어쓰지 않는다. 다른 identity 또는 다른 slug의 선등록 채널은
    -- 수동 대조가 필요하므로 신규 INSERT까지 문장 전체를 롤백한다.
    IF EXISTS (
        SELECT 1
        FROM (VALUES
            ('hyakuto-kyoko', 'UCSjQDxud2HkAO2DVD3lwxmw'),
            ('achichi-mela', 'UC8eitCE9Z6EwUCs-VUi1blg'),
            ('suzuna-tsuzuri', 'UCy9mgxB8pn2C4aNK_MPthDQ'),
            ('sorashina-sopia', 'UCROQtXcp2loQEmvpe5rhJzQ'),
            ('asobi-mawaritai', 'UCAHwWUotyS3l2qBetFDsjgQ')
        ) AS expected(slug, channel_id)
        JOIN members m ON m.slug = expected.slug OR m.channel_id = expected.channel_id
        WHERE (m.slug, m.org, m.channel_id)
            IS DISTINCT FROM (expected.slug, 'Hololive', expected.channel_id)
    ) THEN
        RAISE EXCEPTION 'ASOBI MAWARI-TAI member identity conflict';
    END IF;
END;
$$;
