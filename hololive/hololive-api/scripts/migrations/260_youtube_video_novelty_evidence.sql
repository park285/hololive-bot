-- 일반 영상·최초공개 NEW_VIDEO의 신규성 근거 계약이다. 근거가 부족한 후보는 알리지 않고 보류한다.
-- earliest_baseline_effective_at은 채널별 첫 수락 기준 목록(비어 있지 않은 목록 또는 COMPLETE 빈 목록)의
-- effective 시각이다. 과거 관측으로 소급 backfill하지 않는다. first_positive_effective_at이 0001년인 레거시
-- clock은 실제 최초 관측 시각이 아니므로 기준으로 쓰지 않는다.
-- novelty_pending은 기준 이후 처음 본 영상 후보 중 결정적 근거가 아직 없는 row만 TRUE다. 기존 row는 FALSE로
-- 남기며 다시 평가하지 않는다.
-- youtubejs video_list generation 2는 항목별 player 공개 근거를 포함한다. 새 collector는 generation 2를 요구하므로
-- collector와 API는 coordinated cutover로 함께 전환한다. 이미 2 이상이면 갱신하지 않는다(225와 같은 가드).
ALTER TABLE public.youtube_content_channel_heads
    ADD COLUMN IF NOT EXISTS earliest_baseline_effective_at TIMESTAMPTZ;

ALTER TABLE public.youtube_content_evidence_clocks
    ADD COLUMN IF NOT EXISTS novelty_pending BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE public.observation_contract_generations
SET current_generation = 2,
    updated_by = 'migration-260',
    updated_at = NOW()
WHERE provider = 'youtubejs'
  AND observation_kind = 'video_list'
  AND current_generation = 1;
