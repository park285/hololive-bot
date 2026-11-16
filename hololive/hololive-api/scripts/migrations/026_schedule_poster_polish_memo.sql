-- 관리자가 일정표 칸의 한국어를 고친 시각(edited_at)과 번역 메모(schedule_poster_translation_memo)를 둔다.
-- edited_at은 그 행이나 변경의 한국어 다섯 칸(title_ko, with_ko, place_ko, tags_ko, note_ko)을 관리자가 마지막으로 고친 시각이다.
-- 고친 값은 그 행이나 변경에 그대로 쓴다. 행 묶음을 다시 뽑거나 글을 다시 대응하면 새 행으로 바뀌어 고친 값과 edited_at이 함께 사라진다.
-- 번역 메모는 한 행(id = true)이고, 본문이 있으면 행 번역과 변경 번역 지시문에 참고 절로 붙는다. 본문이 비면 지시문은 이전과 같다.
-- revision은 저장할 때마다 오르고 Console이 읽은 값과 같을 때만 저장한다. updated_at은 저장한 적이 없으면 NULL이다.
-- 이전 image는 이 열과 표를 읽지도 쓰지도 않는다. 되돌릴 때 이 migration은 되돌리지 않는다.
BEGIN;

ALTER TABLE public.schedule_poster_rows ADD COLUMN IF NOT EXISTS edited_at timestamp with time zone;
ALTER TABLE public.schedule_poster_changes ADD COLUMN IF NOT EXISTS edited_at timestamp with time zone;

CREATE TABLE IF NOT EXISTS public.schedule_poster_translation_memo (
    id boolean DEFAULT true NOT NULL,
    body text DEFAULT ''::text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    updated_at timestamp with time zone,
    CONSTRAINT schedule_poster_translation_memo_pkey PRIMARY KEY (id),
    CONSTRAINT chk_schedule_poster_translation_memo_single CHECK (id),
    CONSTRAINT chk_schedule_poster_translation_memo_body CHECK (char_length(body) <= 4000),
    CONSTRAINT chk_schedule_poster_translation_memo_revision CHECK (revision >= 1)
);

INSERT INTO public.schedule_poster_translation_memo (id) VALUES (true) ON CONFLICT (id) DO NOTHING;

COMMIT;
