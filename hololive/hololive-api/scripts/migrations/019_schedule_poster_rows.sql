-- 확정한 포스터에서 뽑은 일정표 행(schedule_poster_rows)과 그 추출 상태(rows_status, rows_error, rows_extracted_at, rows_revision)를 둔다.
-- 번역 이미지와 translation_status는 그대로이고 두 흐름은 서로의 상태를 바꾸지 않는다. 행 추출은 revision을 올리지 않는다.
-- revision이 오르면 몇 시간 걸리는 이미지 번역의 결과 기록(revision 조건)이 버려지기 때문이다.
-- rows_status 기본값이 pending이라 이전 image가 확정한 행도 추출 대상이 되며, 추출은 확정본 가운데 주가 끝나지 않은 것만 본다.
-- rows_revision은 재추출 예약·주 범위 수정·거절마다 오르고, 추출 결과는 읽었을 때와 같을 때만 기록한다.
-- rows_extracted_at은 행 묶음을 마지막으로 저장한 시각이다. 재추출을 예약하거나 재추출이 실패해도 이전 묶음과 함께 남아 계속 게시된다.
-- start_time·end_time은 포스터에 적힌 24시 넘는 시각(25:00, 29:59까지)을 그대로 두려고 text다.
-- JST가 아닌 포스터의 행은 day·start_time·end_time을 JST로 바꿔 tz를 'JST'로, source_zone을 포스터에 적힌 시간대로 저장한다.
-- rows_day_shift(-1..1)는 그렇게 바꾼 행 묶음이 주 범위보다 며칠 밀렸는지이며 행 묶음과 같은 트랜잭션에서 쓰고, 묶음을 지우면 0으로 돌린다.
-- アソビ隊는 포스터가 쓰는 유닛 약칭이다. 번역 이름표가 별칭으로 읽는다.
-- IRyS의 비어 있던 korean_name을 짧은 이름과 같은 표기로 채운다. 번역 이름표는 한국어 이름이 빈 멤버를 빼고, 이미 값이 있으면 덮지 않는다.
-- 이름이 앞에 오는 멤버 12명의 short_korean_name이 성으로 들어가 있어 이름으로 고친다. 지금 값이 그 옛 값일 때만 바꿔 운영자가 고친 값은 덮지 않는다.
-- 이 열은 표시 전용이라 알림·봇 응답·방송 기록·달력 카드의 12명 표시도 함께 바뀌고, 이전 image로 돌아가도 값은 남는다.
-- 이전 image는 이 열과 표를 읽지도 쓰지도 않는다. 이전 image가 거절한 확정본에는 행 묶음과 rows_extracted_at이 남을 수 있어
-- rows_extracted_at을 확정본으로 제한하지 않으며, 행을 읽는 쿼리가 모두 review_status = 'confirmed'로 거른다.
-- 수백 행 이하라 한 트랜잭션에서 만든다.
BEGIN;

ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS rows_status text DEFAULT 'pending'::text NOT NULL;
ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS rows_error text DEFAULT ''::text NOT NULL;
ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS rows_extracted_at timestamp with time zone;
ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS rows_revision bigint DEFAULT 1 NOT NULL;
ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS rows_day_shift smallint DEFAULT 0 NOT NULL;
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_rows_status_vocab;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_rows_status_vocab CHECK (
    rows_status IN ('pending', 'succeeded', 'failed')
);
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_rows_extracted;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_rows_extracted CHECK (
    rows_status <> 'succeeded' OR rows_extracted_at IS NOT NULL
);
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_rows_day_shift;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_rows_day_shift CHECK (rows_day_shift BETWEEN -1 AND 1);

CREATE TABLE IF NOT EXISTS public.schedule_poster_rows (
    poster_id bigint NOT NULL,
    ordinal smallint NOT NULL,
    day date NOT NULL,
    start_time text,
    end_time text,
    tz text DEFAULT ''::text NOT NULL,
    source_zone text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    source_text text NOT NULL,
    badges text[] DEFAULT '{}'::text[] NOT NULL,
    title_ko text NOT NULL,
    with_ko text[] DEFAULT '{}'::text[] NOT NULL,
    place_ko text DEFAULT ''::text NOT NULL,
    tags_ko text[] DEFAULT '{}'::text[] NOT NULL,
    note_ko text DEFAULT ''::text NOT NULL,
    terms jsonb DEFAULT '[]'::jsonb NOT NULL,
    doubt_ko text DEFAULT ''::text NOT NULL,
    warnings text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT schedule_poster_rows_pkey PRIMARY KEY (poster_id, ordinal),
    CONSTRAINT fk_schedule_poster_rows_poster FOREIGN KEY (poster_id) REFERENCES public.schedule_posters (id) ON DELETE CASCADE,
    CONSTRAINT chk_schedule_poster_rows_ordinal CHECK (ordinal BETWEEN 1 AND 100),
    CONSTRAINT chk_schedule_poster_rows_status_vocab CHECK (status IN ('planned', 'tentative', 'cancelled', 'off')),
    CONSTRAINT chk_schedule_poster_rows_time CHECK (
        (start_time IS NULL OR start_time ~ '^([01]?[0-9]|2[0-9]):[0-5][0-9]$')
        AND (end_time IS NULL OR end_time ~ '^([01]?[0-9]|2[0-9]):[0-5][0-9]$')
    ),
    CONSTRAINT chk_schedule_poster_rows_text CHECK (
        char_length(source_text) BETWEEN 1 AND 1000 AND char_length(title_ko) BETWEEN 1 AND 1000
        AND char_length(place_ko) <= 1000 AND char_length(note_ko) <= 1000 AND char_length(doubt_ko) <= 1000
        AND char_length(tz) <= 40 AND char_length(source_zone) <= 40
    ),
    CONSTRAINT chk_schedule_poster_rows_lists CHECK (
        cardinality(badges) <= 40 AND cardinality(with_ko) <= 40 AND cardinality(tags_ko) <= 40 AND cardinality(warnings) <= 40
        AND jsonb_typeof(terms) = 'array'
    )
);

UPDATE public.members
SET aliases = jsonb_set(aliases, '{ja}', (aliases -> 'ja') || '["アソビ隊"]'::jsonb)
WHERE slug = 'asobi-mawaritai' AND NOT (aliases -> 'ja') ? 'アソビ隊';

UPDATE public.members
SET korean_name = '아이리스'
WHERE slug = 'irys' AND NULLIF(korean_name, '') IS NULL;

UPDATE public.members AS m
SET short_korean_name = fix.corrected
FROM (VALUES
    ('aki-rosenthal', '로젠탈', '아키'),
    ('la-darknesss', '다크니스', '라플라스'),
    ('moona-hoshinova', '호시노바', '무나'),
    ('airani-iofifteen', '이오피프틴', '이오피'),
    ('anya-melfissa', '멜피사', '아냐'),
    ('kaela-kovalskia', '코발스키아', '카엘라'),
    ('kobo-kanaeru', '카나에루', '코보'),
    ('shiori-novella', '노벨라', '시오리'),
    ('nerissa-ravencroft', '레이븐크로프트', '네리사'),
    ('elizabeth-rose-bloodflame', '블러드플레임', '엘리자베스'),
    ('cecilia-immergreen', '이머그린', '세실리아'),
    ('raora-panthera', '판테라', '라오라')
) AS fix(slug, current, corrected)
WHERE m.slug = fix.slug AND m.short_korean_name = fix.current;

COMMIT;
