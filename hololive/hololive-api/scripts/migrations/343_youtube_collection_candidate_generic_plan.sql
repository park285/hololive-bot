-- 342의 세대·종류 인덱스를 전제로 collector 후보 조회의 generic plan을 함수 안에 고정한다.
BEGIN;

-- 342의 동시 생성이 중단되면 invalid 인덱스가 남고 IF NOT EXISTS 재실행은 그것을 건너뛴다.
-- 인덱스 없이 generic plan을 고정하면 seq scan이 되므로 함수를 만들기 전에 실패시킨다.
DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_index AS index_state
        WHERE index_state.indexrelid =
            'public.idx_youtube_collection_targets_generation_kind'::pg_catalog.regclass
          AND index_state.indisvalid
          AND index_state.indisready
    ) THEN
        RAISE EXCEPTION 'idx_youtube_collection_targets_generation_kind is missing or invalid; rebuild it before applying 343';
    END IF;
END
$migration$;

-- 본문은 collector repository_candidates_0144_02.sql의 기존 문장을 테이블·열 한정과 이름 있는 인자만 바꿔 옮겼다.
-- SET 절이 있는 SQL 함수는 inline되지 않으므로 PG 18의 SQL 함수 plan cache가 세션 동안 generic plan을 재사용한다.
-- 호출자 snapshot을 그대로 쓰도록 STABLE이다.
CREATE OR REPLACE FUNCTION public.youtube_collection_job_candidates(
    p_generation BIGINT,
    p_kinds TEXT[],
    p_provider TEXT,
    p_job_kind TEXT,
    p_excluded TEXT[],
    p_limit INTEGER
)
RETURNS TABLE (is_current BOOLEAN, subject_key TEXT, min_interval_ms BIGINT, max_interval_ms BIGINT)
LANGUAGE sql
STABLE
SET search_path = pg_catalog
SET plan_cache_mode = force_generic_plan
AS $function$
WITH target_bundles AS (
    SELECT target.subject_key,
           MIN(target.poll_interval_ms) AS min_interval_ms,
           MAX(target.poll_interval_ms) AS max_interval_ms,
           MAX(target.priority) AS max_priority
    FROM public.youtube_collection_targets AS target
    WHERE target.projection_generation = p_generation
      AND target.observation_kind = ANY(p_kinds)
      AND target.enabled = TRUE
    GROUP BY target.subject_key
    -- not_before는 신규 입장 판정에만 쓴다. bundle의 한 행이라도 입장 가능하면 후보다.
    HAVING bool_or(target.not_before IS NULL OR target.not_before <= statement_timestamp())
), due AS (
    SELECT target.subject_key,
           target.min_interval_ms,
           target.max_interval_ms,
           target.max_priority,
           lease.job_key,
           CASE
             WHEN lease.job_key IS NULL THEN '-infinity'::timestamptz
             -- 주기가 줄었으면 이전 긴 주기로 계산된 next_due_at 대신 현재 주기의 다음 slot을 due로 본다.
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
      AND (
           lease.job_key IS NULL
        OR (lease.slot_state = 'IDLE' AND LEAST(
                lease.next_due_at,
                lease.scheduled_for + target.min_interval_ms * INTERVAL '1 millisecond'
            ) <= statement_timestamp())
        OR (lease.slot_state = 'DEFERRED' AND lease.retry_not_before <= statement_timestamp())
        OR (lease.slot_state = 'ACTIVE' AND lease.lease_expires_at <= statement_timestamp())
      )
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
       due.max_interval_ms
FROM projection
LEFT JOIN (
    SELECT due.subject_key,
           due.min_interval_ms,
           due.max_interval_ms,
           due.job_key,
           due.max_priority,
           due.effective_due_at
    FROM due
    -- 새 수명 검토 대상의 최초 discovery가 기존 LIVE 확인을 밀어내지 않는다.
    -- 다른 job 종류의 현행 신규 대상 우선순위는 유지한다.
    ORDER BY CASE WHEN p_job_kind = 'youtubejs_video_live' THEN due.max_priority ELSE 0 END DESC,
             (due.job_key IS NOT NULL), due.max_priority DESC, due.effective_due_at ASC, due.subject_key ASC
    LIMIT p_limit + 1
) AS due ON projection.is_current;
$function$;

REVOKE ALL ON FUNCTION public.youtube_collection_job_candidates(BIGINT, TEXT[], TEXT, TEXT, TEXT[], INTEGER) FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT EXECUTE ON FUNCTION public.youtube_collection_job_candidates(BIGINT, TEXT[], TEXT, TEXT, TEXT[], INTEGER)
            TO hololive_scraper;
    END IF;
END
$migration$;

COMMIT;
