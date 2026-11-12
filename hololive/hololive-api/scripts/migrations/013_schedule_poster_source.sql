-- 유튜브 커뮤니티 글도 일정 포스터 후보로 받기 위해 출처를 기록하고, 출처마다 Iris Console이 받는 게시물·원본 URL 형태를 DB가 지킨다.
-- 커뮤니티 행은 X 사용자가 없어 x_user_id에 빈 문자열을 둔다. NOT NULL을 유지해야 이 열을 문자열로 읽는 이전 image로 되돌릴 수 있다.
-- 수백 행 이하라 한 트랜잭션에서 검증한다.
BEGIN;

ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS source text DEFAULT 'x'::text NOT NULL;
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_source;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_source CHECK (
    (source = 'x' AND x_user_id <> '' AND post_url ~ '^https://x\.com/i/status/[1-9][0-9]*$' AND image_url LIKE 'https://pbs.twimg.com/%')
    OR (source = 'youtube_community' AND x_user_id = '' AND post_url ~ '^https://www\.youtube\.com/post/[A-Za-z0-9_-]+$'
        AND image_url LIKE 'https://yt3.ggpht.com/%')
);

COMMIT;
