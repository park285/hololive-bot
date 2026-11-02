-- 366의 content absence slot 보존 정리가 scheduled_for 순으로 잘라 지우는 데 쓴다. 기존 PK는 channel_id가 선두다.
-- 2026-11-02 운영: 삭제 경로 없이 하루 2,881행씩 늘어 131,899행(그중 14일 지난 행 91,578)이었다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_content_absence_slots_scheduled_for
    ON public.youtube_content_absence_slots (scheduled_for);
