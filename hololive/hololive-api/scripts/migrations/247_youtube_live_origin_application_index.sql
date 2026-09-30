-- 수명 출처 분류에서 보존 application 전체를 영상/배치마다 스캔하지 않는다.
-- 원시 observation retention 뒤 남는 확정 application도 같은 lookup을 사용한다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_source_application_live_origin
    ON public.source_observation_applications (entity_key)
    WHERE entity_kind = 'youtube_live_session'
      AND decision IN ('APPLIED', 'ENDED')
      AND observation_kind IN ('live_snapshot', 'video_live_check');
