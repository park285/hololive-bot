-- 제목의 관측 시각은 일정·상태 관측과 독립적이다. 기존 제목의 시각을 추정 backfill하지 않는다.
ALTER TABLE public.youtube_live_sessions
    ADD COLUMN IF NOT EXISTS title_observed_at TIMESTAMPTZ;
