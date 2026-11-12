-- 멤버가 프리챗 영상 썸네일로 올리는 주간 일정도 포스터 후보로 받는다. 썸네일은 같은 주소에서 그림만 바뀌므로,
-- 후보를 만들 때 받은 그림의 sha256을 image_sha256에 남겨 번역이 같은 그림인지 확인한다. 이전 image는 이 열을 읽지 않는다.
-- 수백 행 이하라 한 트랜잭션에서 검증한다.
BEGIN;

ALTER TABLE public.schedule_posters ADD COLUMN IF NOT EXISTS image_sha256 text;
ALTER TABLE public.schedule_posters DROP CONSTRAINT IF EXISTS chk_schedule_posters_source;
ALTER TABLE public.schedule_posters ADD CONSTRAINT chk_schedule_posters_source CHECK (
    (source = 'x' AND x_user_id <> '' AND post_url ~ '^https://x\.com/i/status/[1-9][0-9]*$' AND image_url LIKE 'https://pbs.twimg.com/%'
        AND image_sha256 IS NULL)
    OR (source = 'youtube_community' AND x_user_id = '' AND post_url ~ '^https://www\.youtube\.com/post/[A-Za-z0-9_-]+$'
        AND image_url LIKE 'https://yt3.ggpht.com/%' AND image_sha256 IS NULL)
    OR (source = 'youtube_thumbnail' AND x_user_id = '' AND post_url ~ '^https://www\.youtube\.com/watch\?v=[A-Za-z0-9_-]{11}$'
        AND image_url = 'https://i.ytimg.com/vi/' || substr(post_url, 33) || '/maxresdefault.jpg'
        AND image_sha256 IS NOT NULL AND image_sha256 ~ '^[0-9a-f]{64}$')
);

COMMIT;
