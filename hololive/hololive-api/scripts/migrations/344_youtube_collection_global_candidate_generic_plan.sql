-- 342의 세대·종류 인덱스를 전제로 collector global 후보 조회와 lease membership 판정의 generic plan을 고정한다.
BEGIN;

-- 인덱스 없이 generic plan을 고정하면 targets seq scan이 되므로 함수를 바꾸기 전에 실패시킨다.
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
        RAISE EXCEPTION 'idx_youtube_collection_targets_generation_kind is missing or invalid; rebuild it before applying 344';
    END IF;
END
$migration$;

-- 본문은 collector repository_candidates_global_0144_17.sql의 기존 문장을 테이블·열 한정과 이름 있는 인자만 바꿔 옮겼다.
-- (NOT p_exact_subject OR subject_key = p_subject_key) 때문에 custom plan 추정이 항상 낮아 매 호출 재계획하던 문장이다.
-- SET 절이 있는 SQL 함수는 inline되지 않으므로 PG 18의 SQL 함수 plan cache가 세션 동안 generic plan을 재사용한다.
-- 호출자 snapshot을 그대로 쓰도록 STABLE이다.
CREATE OR REPLACE FUNCTION public.youtube_collection_job_global_candidates(
    p_generation BIGINT,
    p_kinds TEXT[],
    p_exact_subject BOOLEAN,
    p_subject_key TEXT,
    p_job_key TEXT,
    p_excluded TEXT[]
)
RETURNS TABLE (is_current BOOLEAN, subject_key TEXT, min_interval_ms BIGINT, max_interval_ms BIGINT)
LANGUAGE sql
STABLE
SET search_path = pg_catalog
SET plan_cache_mode = force_generic_plan
AS $function$
WITH target_bundle AS (
    SELECT COUNT(target.subject_key) AS target_count,
           MIN(target.poll_interval_ms) AS min_interval_ms,
           MAX(target.poll_interval_ms) AS max_interval_ms,
           MAX(target.priority) AS max_priority,
           -- not_before는 신규 입장 판정에만 쓴다. bundle의 한 행이라도 입장 가능하면 후보다.
           COUNT(target.subject_key) FILTER (
               WHERE target.not_before IS NULL OR target.not_before <= statement_timestamp()
           ) AS admissible_count
    FROM public.youtube_collection_targets AS target
    WHERE target.projection_generation = p_generation
      AND target.observation_kind = ANY(p_kinds)
      AND target.enabled = TRUE
      AND (NOT p_exact_subject OR target.subject_key = p_subject_key)
), identity AS (
    SELECT p_job_key AS job_key,
           p_subject_key AS subject_key,
           target_bundle.target_count,
           target_bundle.min_interval_ms,
           target_bundle.max_interval_ms,
           target_bundle.max_priority,
           target_bundle.admissible_count
    FROM target_bundle
), due AS (
    SELECT identity.job_key,
           identity.subject_key,
           identity.target_count,
           identity.min_interval_ms,
           identity.max_interval_ms,
           identity.max_priority,
           lease.slot_state,
           lease.next_due_at,
           lease.retry_not_before,
           lease.lease_expires_at
    FROM identity
    LEFT JOIN public.youtube_collection_job_leases AS lease
      ON lease.job_key = identity.job_key
    WHERE identity.target_count > 0
      AND identity.admissible_count > 0
      AND identity.job_key <> ALL(p_excluded)
      AND (
           lease.job_key IS NULL
        -- 주기가 줄었으면 이전 긴 주기로 계산된 next_due_at 대신 현재 주기의 다음 slot을 due로 본다.
        OR (lease.slot_state = 'IDLE' AND LEAST(
                lease.next_due_at,
                lease.scheduled_for + identity.min_interval_ms * INTERVAL '1 millisecond'
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
LEFT JOIN due ON projection.is_current;
$function$;

REVOKE ALL ON FUNCTION public.youtube_collection_job_global_candidates(BIGINT, TEXT[], BOOLEAN, TEXT, TEXT, TEXT[]) FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT EXECUTE ON FUNCTION public.youtube_collection_job_global_candidates(BIGINT, TEXT[], BOOLEAN, TEXT, TEXT, TEXT[])
            TO hololive_scraper;
    END IF;
END
$migration$;

-- lease membership 검사(collector repository_lease_membership_0004_04.sql)가 lease마다 부르는 함수다.
-- 같은 OR 조건 때문에 세션 plan cache가 매 호출 custom plan을 세웠다(운영 0.4~0.7ms).
-- generic plan은 342 인덱스의 (세대, 종류) 범위를 읽고 subject를 filter로 거른다(운영 0.2~0.5ms).
ALTER FUNCTION public.youtube_collection_membership_valid(TEXT[], BOOLEAN, TEXT, INTEGER, BIGINT)
    SET plan_cache_mode = force_generic_plan;

COMMIT;
