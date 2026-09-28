-- 명시적으로 승인한 과거 projection 복구만 수행한다. preview 원본을 private 파일에 보존한다.
-- psql -v expected_count=N -v expected_digest=PREVIEW_DIGEST -f THIS_FILE
\set ON_ERROR_STOP on
\if :{?expected_count}
\else
  \echo 'expected_count is required'
  \quit 3
\endif
\if :{?expected_digest}
\else
  \echo 'expected_digest is required'
  \quit 3
\endif
BEGIN;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '3s';
SET LOCAL TIME ZONE 'UTC';
SELECT set_config('iris.live_head_repair.expected_count', :'expected_count', true);
SELECT set_config('iris.live_head_repair.expected_digest', :'expected_digest', true);
DO $$
BEGIN
    IF current_database() <> 'hololive' THEN
        RAISE EXCEPTION 'wrong database for terminal head repair';
    END IF;
    IF current_setting('iris.live_head_repair.expected_count') !~ '^[1-9][0-9]{0,2}$'
       OR current_setting('iris.live_head_repair.expected_count')::integer > 100
       OR current_setting('iris.live_head_repair.expected_digest') !~ '^[0-9a-f]{32}$' THEN
        RAISE EXCEPTION 'invalid bounded snapshot precondition';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_trigger
               WHERE tgrelid IN ('public.youtube_live_sessions'::regclass,
                                 'public.youtube_live_reconciliation_heads'::regclass)
                 AND NOT tgisinternal AND tgenabled <> 'D')
       OR EXISTS (SELECT 1 FROM pg_rules WHERE schemaname = 'public'
                  AND tablename IN ('youtube_live_sessions', 'youtube_live_reconciliation_heads')) THEN
        RAISE EXCEPTION 'unreviewed trigger or rule on repair tables';
    END IF;
END $$;
CREATE TEMP TABLE terminal_head_repair ON COMMIT DROP AS
SELECT h.video_id, to_jsonb(h) AS original_head, p.ended_at AS canonical_end
FROM public.youtube_live_sessions p
JOIN public.youtube_live_reconciliation_heads h USING (video_id)
WHERE p.status = 'ENDED' AND h.status = 'LIVE'
  AND p.ended_at IS NOT NULL AND h.last_live_positive_at IS NOT NULL
  AND p.ended_at >= GREATEST(h.last_live_positive_at, h.last_upcoming_positive_at)
  AND h.updated_at < TIMESTAMPTZ '2026-08-17 00:00:00+00'
  AND h.ended_at IS NULL AND h.end_reason IS NULL
  AND h.end_candidate_kind IS NULL AND h.end_candidate_observation_id IS NULL
  AND h.next_end_check_at IS NULL
ORDER BY h.video_id LIMIT 101;
DO $$
DECLARE
    expected_count integer := current_setting('iris.live_head_repair.expected_count')::integer;
    actual_count integer;
    actual_digest text;
    changed integer;
BEGIN
    -- 정상 writer와 같은 순서: 정본 session → head, 각 단계는 video_id 순서.
    PERFORM p.video_id FROM public.youtube_live_sessions p
    JOIN terminal_head_repair t USING (video_id) ORDER BY p.video_id FOR UPDATE OF p;
    PERFORM h.video_id FROM public.youtube_live_reconciliation_heads h
    JOIN terminal_head_repair t USING (video_id) ORDER BY h.video_id FOR UPDATE OF h;

    -- 잠금 대기 중 변경을 재검사하고 잠금 획득 뒤의 원본으로만 차분을 검증한다.
    UPDATE terminal_head_repair t SET original_head = to_jsonb(h), canonical_end = p.ended_at
    FROM public.youtube_live_sessions p JOIN public.youtube_live_reconciliation_heads h USING (video_id)
    WHERE t.video_id = h.video_id;
    SELECT count(h.video_id), md5(COALESCE(jsonb_agg(jsonb_build_object(
        'head', to_jsonb(h), 'canonical_end', p.ended_at) ORDER BY h.video_id), '[]'::jsonb)::text)
    INTO actual_count, actual_digest
    FROM terminal_head_repair t
    JOIN public.youtube_live_sessions p USING (video_id)
    JOIN public.youtube_live_reconciliation_heads h USING (video_id)
    WHERE p.status = 'ENDED' AND h.status = 'LIVE'
      AND p.ended_at IS NOT NULL AND h.last_live_positive_at IS NOT NULL
      AND p.ended_at >= GREATEST(h.last_live_positive_at, h.last_upcoming_positive_at)
      AND h.updated_at < TIMESTAMPTZ '2026-08-17 00:00:00+00'
      AND h.ended_at IS NULL AND h.end_reason IS NULL
      AND h.end_candidate_kind IS NULL AND h.end_candidate_observation_id IS NULL
      AND h.next_end_check_at IS NULL;
    IF actual_count <> expected_count
       OR (SELECT count(video_id) FROM terminal_head_repair) <> expected_count
       OR actual_digest <> current_setting('iris.live_head_repair.expected_digest') THEN
        RAISE EXCEPTION 'terminal head snapshot changed; no rows repaired';
    END IF;
    UPDATE public.youtube_live_reconciliation_heads h
    SET status = 'ENDED', ended_at = t.canonical_end, updated_at = statement_timestamp()
    FROM terminal_head_repair t WHERE h.video_id = t.video_id;
    GET DIAGNOSTICS changed = ROW_COUNT;
    IF changed <> expected_count OR EXISTS (
        SELECT 1 FROM terminal_head_repair t
        JOIN public.youtube_live_reconciliation_heads h USING (video_id)
        WHERE h.status <> 'ENDED' OR h.ended_at IS DISTINCT FROM t.canonical_end
           OR (to_jsonb(h) - ARRAY['status','ended_at','updated_at']) IS DISTINCT FROM
              (t.original_head - ARRAY['status','ended_at','updated_at'])
    ) THEN
        RAISE EXCEPTION 'terminal head repair postcondition failed';
    END IF;
END $$;
SELECT jsonb_build_object('updated_count', count(h.video_id), 'after_digest', md5(jsonb_agg(
    to_jsonb(h) ORDER BY h.video_id)::text))
FROM terminal_head_repair t JOIN public.youtube_live_reconciliation_heads h USING (video_id);
COMMIT;
