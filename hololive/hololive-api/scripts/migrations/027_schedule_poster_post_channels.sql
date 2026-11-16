-- 글 하나를 여러 멤버의 일정에 반영할 수 있게 글 원장(schedule_poster_posts)의 기본 키를 (post_id, channel_id)로 넓힌다.
-- 같은 post_id의 행이 멤버마다 따로 판별·대응·철회된다. 글쓴이의 행은 글 본문을, 그 글이 알린 방송의 다른 참가자 행은 공식 일정 내용을 본문으로 가진다.
-- 변경(schedule_poster_changes)은 channel_id를 더해 그 멤버의 글 행에 묶는다. target_post_id는 같은 channel_id의 앞선 글을 가리킨다.
-- 두 표는 수천 행 이하라 기본 키를 한 트랜잭션에서 다시 만든다.
-- 이전 image는 글 수집(ON CONFLICT (post_id))과 변경 기록(channel_id 없는 INSERT)이 실패한다. 일정표 행과 !스케쥴 게시는 그대로다.
-- 되돌릴 때 이 migration은 되돌리지 않으므로, 이전 image로 돌아가면 다시 올릴 때까지 일정 변경 감지가 멈춘다.
BEGIN;

ALTER TABLE public.schedule_poster_changes ADD COLUMN IF NOT EXISTS channel_id character varying(64);

UPDATE public.schedule_poster_changes c
SET channel_id = d.channel_id
FROM public.schedule_poster_posts d
WHERE d.post_id = c.post_id AND c.channel_id IS NULL;

ALTER TABLE public.schedule_poster_changes DROP CONSTRAINT IF EXISTS fk_schedule_poster_changes_post;
ALTER TABLE public.schedule_poster_changes DROP CONSTRAINT IF EXISTS schedule_poster_changes_pkey;
ALTER TABLE public.schedule_poster_posts DROP CONSTRAINT IF EXISTS schedule_poster_posts_pkey;

ALTER TABLE public.schedule_poster_posts
    ADD CONSTRAINT schedule_poster_posts_pkey PRIMARY KEY (post_id, channel_id);
ALTER TABLE public.schedule_poster_changes
    ADD CONSTRAINT schedule_poster_changes_pkey PRIMARY KEY (post_id, channel_id, item);
ALTER TABLE public.schedule_poster_changes
    ADD CONSTRAINT fk_schedule_poster_changes_post FOREIGN KEY (post_id, channel_id)
        REFERENCES public.schedule_poster_posts (post_id, channel_id) ON DELETE CASCADE;

COMMIT;
