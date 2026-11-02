-- 366의 profile evidence 보존 정리가 received_at 순으로 잘라 지우는 데 쓴다. 기존 PK는 channel_id가 선두다.
-- 2026-11-02 운영: 삭제 경로 없이 하루 476행씩 늘었다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_channel_profile_evidence_received_at
    ON public.youtube_channel_profile_evidence (received_at);
