-- 규칙과 영상별 검토가 미분류로 남긴 종료 방송을 Gemini Decisions API에 물은 답이다. 행은 title로 물은 답이고,
-- 방송 이력은 현재 제목이 같은 행만 쓰며 bot plane이 제목이 바뀐 방송을 다시 묻는다.
-- decided는 고른 유형과 그 확률이고, 신뢰도 문턱은 읽는 쪽이 정하므로 낮은 답도 남긴다. blocked는 입력 안전 차단이라 답이 없다.
-- 새 표라 한 트랜잭션에서 만든다.
BEGIN;

CREATE TABLE IF NOT EXISTS public.youtube_broadcast_type_decisions (
    video_id character varying(20) NOT NULL,
    title character varying(500) NOT NULL,
    outcome text NOT NULL,
    broadcast_type text,
    confidence double precision,
    model text NOT NULL,
    decided_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT youtube_broadcast_type_decisions_pkey PRIMARY KEY (video_id),
    CONSTRAINT youtube_broadcast_type_decisions_video_id_fkey FOREIGN KEY (video_id)
        REFERENCES public.youtube_live_sessions(video_id) ON DELETE CASCADE,
    CONSTRAINT chk_youtube_broadcast_type_decisions_outcome_vocab CHECK (outcome = ANY (ARRAY['decided'::text, 'blocked'::text])),
    CONSTRAINT chk_youtube_broadcast_type_decisions_type_vocab CHECK (broadcast_type IS NULL OR broadcast_type = ANY (ARRAY[
        'game'::text, 'talk'::text, 'singing'::text, 'asmr'::text, 'membership'::text, 'event'::text,
        'horse_racing'::text, 'watchalong'::text, 'news'::text, 'other'::text])),
    CONSTRAINT chk_youtube_broadcast_type_decisions_answer CHECK (
        (outcome = 'decided') = (broadcast_type IS NOT NULL) AND (broadcast_type IS NULL) = (confidence IS NULL)),
    CONSTRAINT chk_youtube_broadcast_type_decisions_confidence CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    CONSTRAINT chk_youtube_broadcast_type_decisions_model CHECK (length(model) BETWEEN 1 AND 64)
);

COMMIT;
