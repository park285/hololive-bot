-- GAP3-02: video_list·shorts_list reduce가 읽는 `scheduled_for = $3 OR effective_at > $4`의 effective_at 갈래는
-- PK (channel_id, observation_kind, scheduled_for)로 범위를 좁힐 수 없어 채널·kind의 slot 이력 전체를 heap에서 읽었다.
-- 이 인덱스로 planner가 PK 등치와 effective_at 범위를 BitmapOr로 결합해, 늦은 slot이 거의 없는 평상시 읽기가
-- 이력 길이와 무관해진다. retention(보존 하한)은 별도 결정이다.
-- CONCURRENTLY는 BEGIN 블록 밖 autocommit 문장이어야 한다(CONVENTIONS). IF NOT EXISTS로 재적용이 멱등이다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_youtube_content_absence_slots_effective
    ON youtube_content_absence_slots (channel_id, observation_kind, effective_at);
