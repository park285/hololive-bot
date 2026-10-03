-- 일정 항목의 실제 관측 시각을 보존하여 재시도·재생 순서가 최신 정본을 되돌리지 않게 한다.
-- 기존 항목의 시각은 추정하지 않으며 다음 관측부터 채운다.
ALTER TABLE public.youtube_schedule_items
    ADD COLUMN IF NOT EXISTS observed_at TIMESTAMPTZ;
