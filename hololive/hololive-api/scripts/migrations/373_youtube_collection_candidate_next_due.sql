-- 구 collector는 네 열을 명시하므로 같은 이름의 다섯 번째 반환 열을 읽지 않는다.
-- DROP과 재정의를 한 트랜잭션으로 묶어 함수가 없는 중간 상태를 노출하지 않는다.
BEGIN;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_index AS index_state
        WHERE index_state.indexrelid =
            pg_catalog.to_regclass('public.idx_youtube_collection_targets_generation_kind')
          AND index_state.indisvalid
          AND index_state.indisready
    ) THEN
        RAISE EXCEPTION 'idx_youtube_collection_targets_generation_kind is missing or invalid';
    END IF;
END
$migration$;

DROP FUNCTION IF EXISTS public.youtube_collection_job_candidates(BIGINT, TEXT[], TEXT, TEXT, TEXT[], INTEGER);
CREATE FUNCTION public.youtube_collection_job_candidates(
    p_generation BIGINT,
    p_kinds TEXT[],
    p_provider TEXT,
    p_job_kind TEXT,
    p_excluded TEXT[],
    p_limit INTEGER
)
RETURNS TABLE (is_current BOOLEAN, subject_key TEXT, min_interval_ms BIGINT, max_interval_ms BIGINT, next_due_at TIMESTAMPTZ)
LANGUAGE sql
STABLE
SET search_path = pg_catalog
SET plan_cache_mode = force_generic_plan
AS $function$
WITH target_bundles AS (
    SELECT target.subject_key,
           MIN(target.poll_interval_ms) AS min_interval_ms,
           MAX(target.poll_interval_ms) AS max_interval_ms,
           MAX(target.priority) AS max_priority,
           MIN(COALESCE(target.not_before, '-infinity'::timestamptz)) AS admitted_at
    FROM public.youtube_collection_targets AS target
    WHERE target.projection_generation = p_generation
      AND target.observation_kind = ANY(p_kinds)
      AND target.enabled = TRUE
    GROUP BY target.subject_key
), bundle_due AS (
    SELECT target.subject_key,
           target.min_interval_ms,
           target.max_interval_ms,
           target.max_priority,
           target.admitted_at,
           lease.job_key,
           CASE
             WHEN lease.job_key IS NULL THEN '-infinity'::timestamptz
             WHEN lease.slot_state = 'IDLE' THEN LEAST(
                 lease.next_due_at,
                 lease.scheduled_for + target.min_interval_ms * INTERVAL '1 millisecond'
             )
             WHEN lease.slot_state = 'DEFERRED' THEN lease.retry_not_before
             ELSE lease.lease_expires_at
           END AS effective_due_at
    FROM target_bundles AS target
    LEFT JOIN public.youtube_collection_job_leases AS lease
      ON lease.job_key = 'collector:' || p_provider || ':' || p_job_kind || ':' || target.subject_key
    WHERE ('collector:' || p_provider || ':' || p_job_kind || ':' || target.subject_key)
          <> ALL(p_excluded)
), hint AS (
    SELECT MIN(GREATEST(admitted_at, effective_due_at)) FILTER (
        WHERE GREATEST(admitted_at, effective_due_at) > statement_timestamp()
    ) AS next_due_at
    FROM bundle_due
), projection AS (
    SELECT EXISTS (
        SELECT 1
        FROM public.youtube_collection_projection_generations AS generation
        WHERE generation.generation = p_generation
          AND generation.status = 'CURRENT'
          AND generation.valid_until > statement_timestamp()
    ) AS is_current
)
SELECT projection.is_current,
       due.subject_key,
       due.min_interval_ms,
       due.max_interval_ms,
       CASE WHEN projection.is_current THEN hint.next_due_at END
FROM projection
CROSS JOIN hint
LEFT JOIN (
    SELECT due.subject_key,
           due.min_interval_ms,
           due.max_interval_ms,
           due.job_key,
           due.max_priority,
           due.effective_due_at
    FROM bundle_due AS due
    WHERE due.admitted_at <= statement_timestamp()
      AND due.effective_due_at <= statement_timestamp()
    ORDER BY CASE WHEN p_job_kind = 'youtubejs_video_live' THEN due.max_priority ELSE 0 END DESC,
             (due.job_key IS NOT NULL), due.max_priority DESC, due.effective_due_at ASC, due.subject_key ASC
    LIMIT p_limit + 1
) AS due ON projection.is_current;
$function$;

DROP FUNCTION IF EXISTS public.youtube_collection_job_global_candidates(BIGINT, TEXT[], BOOLEAN, TEXT, TEXT, TEXT[]);
CREATE FUNCTION public.youtube_collection_job_global_candidates(
    p_generation BIGINT,
    p_kinds TEXT[],
    p_exact_subject BOOLEAN,
    p_subject_key TEXT,
    p_job_key TEXT,
    p_excluded TEXT[]
)
RETURNS TABLE (is_current BOOLEAN, subject_key TEXT, min_interval_ms BIGINT, max_interval_ms BIGINT, next_due_at TIMESTAMPTZ)
LANGUAGE sql
STABLE
SET search_path = pg_catalog
SET plan_cache_mode = force_generic_plan
AS $function$
WITH target_bundle AS (
    SELECT COUNT(target.subject_key) AS target_count,
           MIN(target.poll_interval_ms) AS min_interval_ms,
           MAX(target.poll_interval_ms) AS max_interval_ms,
           MIN(COALESCE(target.not_before, '-infinity'::timestamptz)) AS admitted_at
    FROM public.youtube_collection_targets AS target
    WHERE target.projection_generation = p_generation
      AND target.observation_kind = ANY(p_kinds)
      AND target.enabled = TRUE
      AND (NOT p_exact_subject OR target.subject_key = p_subject_key)
), bundle_due AS (
    SELECT p_subject_key AS subject_key,
           target.min_interval_ms,
           target.max_interval_ms,
           GREATEST(target.admitted_at, CASE
             WHEN lease.job_key IS NULL THEN '-infinity'::timestamptz
             WHEN lease.slot_state = 'IDLE' THEN LEAST(
                 lease.next_due_at,
                 lease.scheduled_for + target.min_interval_ms * INTERVAL '1 millisecond'
             )
             WHEN lease.slot_state = 'DEFERRED' THEN lease.retry_not_before
             ELSE lease.lease_expires_at
           END) AS candidate_at
    FROM target_bundle AS target
    LEFT JOIN public.youtube_collection_job_leases AS lease ON lease.job_key = p_job_key
    WHERE target.target_count > 0
      AND p_job_key <> ALL(p_excluded)
), hint AS (
    SELECT MIN(candidate_at) FILTER (WHERE candidate_at > statement_timestamp()) AS next_due_at
    FROM bundle_due
), projection AS (
    SELECT EXISTS (
        SELECT 1
        FROM public.youtube_collection_projection_generations AS generation
        WHERE generation.generation = p_generation
          AND generation.status = 'CURRENT'
          AND generation.valid_until > statement_timestamp()
    ) AS is_current
)
SELECT projection.is_current,
       due.subject_key,
       due.min_interval_ms,
       due.max_interval_ms,
       CASE WHEN projection.is_current THEN hint.next_due_at END
FROM projection
CROSS JOIN hint
LEFT JOIN bundle_due AS due
  ON projection.is_current AND due.candidate_at <= statement_timestamp();
$function$;

REVOKE ALL ON FUNCTION public.youtube_collection_job_candidates(BIGINT, TEXT[], TEXT, TEXT, TEXT[], INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.youtube_collection_job_global_candidates(BIGINT, TEXT[], BOOLEAN, TEXT, TEXT, TEXT[]) FROM PUBLIC;
DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT EXECUTE ON FUNCTION public.youtube_collection_job_candidates(BIGINT, TEXT[], TEXT, TEXT, TEXT[], INTEGER) TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.youtube_collection_job_global_candidates(BIGINT, TEXT[], BOOLEAN, TEXT, TEXT, TEXT[]) TO hololive_scraper;
    END IF;
END
$migration$;

COMMIT;
