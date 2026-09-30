-- 기존 행은 출처 미상으로 보존한다. 신규 owner writer는 출처를 명시하며,
-- 구버전 writer의 누락 입력도 metadata_only로 면제하지 않는다.
ALTER TABLE public.youtube_live_sessions
    ADD COLUMN IF NOT EXISTS lifecycle_origin TEXT NOT NULL DEFAULT 'legacy_unknown';

DO $constraint$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_constraint
        WHERE conrelid = 'public.youtube_live_sessions'::regclass
          AND conname = 'chk_youtube_live_sessions_lifecycle_origin_vocab'
    ) THEN
        ALTER TABLE public.youtube_live_sessions
            ADD CONSTRAINT chk_youtube_live_sessions_lifecycle_origin_vocab
            CHECK (lifecycle_origin IN ('metadata_only', 'observed', 'legacy_unknown')) NOT VALID;
    END IF;
END
$constraint$;
ALTER TABLE public.youtube_live_sessions
    VALIDATE CONSTRAINT chk_youtube_live_sessions_lifecycle_origin_vocab;

-- 전체 원장을 keyset으로 읽되, 각 최대 1000행의 갱신은 독립 commit한다.
-- 단순 head 존재는 과거 부재 처리로 생성됐을 수 있어 출처 증명이 아니다.
-- 실제 positive clock 쌍 또는 수명 consumer의 확정 application만 사용한다.
CREATE OR REPLACE PROCEDURE public.backfill_youtube_live_lifecycle_origin()
LANGUAGE plpgsql
AS $backfill$
DECLARE
    previous_video_id TEXT;
    candidate_ids TEXT[];
BEGIN
    -- 같은 원시 원장을 세션/배치마다 재스캔하지 않는다. application의 kind와
    -- 확정 결정은 원시 observation retention 뒤에도 남는 출처 증거다.
    DROP TABLE IF EXISTS pg_temp.youtube_lifecycle_origin_evidence;
	IF NOT EXISTS (
		SELECT 1 FROM pg_catalog.pg_index
		WHERE indexrelid = 'public.idx_source_application_live_origin'::regclass
		  AND indisvalid AND indisready
	) THEN
		RAISE EXCEPTION 'live origin application index is invalid; rebuild failed concurrent index before resuming';
	END IF;
    CREATE TEMP TABLE youtube_lifecycle_origin_evidence (video_id TEXT PRIMARY KEY)
        ON COMMIT PRESERVE ROWS;
    INSERT INTO pg_temp.youtube_lifecycle_origin_evidence (video_id)
    SELECT session.video_id FROM public.youtube_live_sessions session
    LEFT JOIN public.youtube_live_reconciliation_heads head USING(video_id)
    WHERE session.lifecycle_origin = 'legacy_unknown'
      AND (
          (head.last_upcoming_positive_at IS NOT NULL AND head.last_upcoming_positive_seen_at IS NOT NULL)
          OR (head.last_live_positive_at IS NOT NULL AND head.last_live_positive_seen_at IS NOT NULL)
          OR EXISTS (
              SELECT 1 FROM public.source_observation_applications application
              WHERE application.entity_key = session.video_id
                AND application.entity_kind = 'youtube_live_session'
                AND application.decision IN ('APPLIED', 'ENDED')
                AND application.observation_kind IN ('live_snapshot', 'video_live_check')
          )
      );

    LOOP
        SELECT array_agg(candidate.video_id ORDER BY candidate.video_id)
        INTO candidate_ids
        FROM (
            SELECT video_id FROM public.youtube_live_sessions
            WHERE previous_video_id IS NULL OR video_id > previous_video_id
            ORDER BY video_id LIMIT 1000
        ) candidate;
        EXIT WHEN candidate_ids IS NULL;

        UPDATE public.youtube_live_sessions session
        SET lifecycle_origin = 'observed'
        WHERE session.video_id = ANY(candidate_ids)
          AND session.lifecycle_origin = 'legacy_unknown'
          AND EXISTS (
              SELECT 1 FROM pg_temp.youtube_lifecycle_origin_evidence evidence
              WHERE evidence.video_id = session.video_id
          );

        previous_video_id := candidate_ids[array_length(candidate_ids, 1)];
        COMMIT;
    END LOOP;
    DROP TABLE pg_temp.youtube_lifecycle_origin_evidence;
END
$backfill$;
REVOKE ALL ON PROCEDURE public.backfill_youtube_live_lifecycle_origin() FROM PUBLIC;
CALL public.backfill_youtube_live_lifecycle_origin();
DROP PROCEDURE IF EXISTS public.backfill_youtube_live_lifecycle_origin();
