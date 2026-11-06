-- 보존 보호 판정은 observation_id만으로 pending-end를 찾는다. 기존 복합 unique는 video_id가 선두다.
-- 제약·보존 조건·기존 인덱스는 유지하고, 반복되는 미존재 조회의 전체 순회를 줄인다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_live_pending_end_observation
    ON public.youtube_live_pending_ends (observation_id);
