-- 수집 job의 유효성을 전체 projection generation 교체와 분리한다.
-- target은 같은 subject/kind/priority/poll/enabled가 이어지는 동안 처음 들어온 generation을
-- member_since_generation으로 보존하고, 새·변경·재등록 target은 새 generation identity를 받는다.
-- generation은 IDENTITY라 재사용되지 않으며 retention 삭제와 무관하다. not_before는 신규 후보 선정과
-- acquire에서만 쓰는 다음 확인 가능 시각이며 projection hash와 job 유효성에 포함하지 않는다.
ALTER TABLE public.youtube_collection_targets
    ADD COLUMN IF NOT EXISTS member_since_generation BIGINT,
    ADD COLUMN IF NOT EXISTS not_before TIMESTAMPTZ;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_constraint
        WHERE conrelid = 'public.youtube_collection_targets'::regclass
          AND conname = 'chk_youtube_collection_target_member_since'
    ) THEN
        -- 과거 generation의 NULL은 허용한다. 새 collector는 NULL을 유효 membership으로 보지 않는다.
        ALTER TABLE public.youtube_collection_targets
            ADD CONSTRAINT chk_youtube_collection_target_member_since CHECK (
                member_since_generation IS NULL
                OR member_since_generation BETWEEN 1 AND projection_generation
            ) NOT VALID;
    END IF;
END
$migration$;

ALTER TABLE public.youtube_collection_targets
    VALIDATE CONSTRAINT chk_youtube_collection_target_member_since;

-- acquire 시점의 job 범위와 필수 target 수를 lease에 기록한다. 기본값(빈 kind·0개)은
-- 새 유효성 함수에서 항상 거짓이므로 이전 collector가 잡은 lease와 fixture는 fail closed다.
ALTER TABLE public.youtube_collection_job_leases
    ADD COLUMN IF NOT EXISTS membership_kinds TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD COLUMN IF NOT EXISTS membership_exact_subject BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS membership_target_count INTEGER NOT NULL DEFAULT 0;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_constraint
        WHERE conrelid = 'public.youtube_collection_job_leases'::regclass
          AND conname = 'chk_youtube_collection_job_membership_count'
    ) THEN
        ALTER TABLE public.youtube_collection_job_leases
            ADD CONSTRAINT chk_youtube_collection_job_membership_count
            CHECK (membership_target_count >= 0) NOT VALID;
    END IF;
END
$migration$;

ALTER TABLE public.youtube_collection_job_leases
    VALIDATE CONSTRAINT chk_youtube_collection_job_membership_count;

-- 현재 CURRENT의 bounded target 집합만 초기화한다. 과거 generation은 backfill하지 않는다.
-- 이전 API·collector를 drain한 coordinated cutover에서 적용하며, 이전 writer와의 혼합 실행은 지원하지 않는다.
UPDATE public.youtube_collection_targets AS target
SET member_since_generation = target.projection_generation
FROM public.youtube_collection_projection_generations AS projection
WHERE projection.status = 'CURRENT'
  AND target.projection_generation = projection.generation
  AND target.member_since_generation IS NULL;

BEGIN;

-- API refresh는 CURRENT를 읽거나 바꾸기 전에 이 row를 FOR UPDATE로, collector는 lock 함수로
-- FOR SHARE로 잡는다. CURRENT 전환 중 대기한 collector가 RETIRED row를 받아 0행이 되는 경합을 막는다.
CREATE TABLE IF NOT EXISTS public.youtube_collection_projection_guard (
    guard_key BOOLEAN PRIMARY KEY DEFAULT TRUE,
    CONSTRAINT chk_youtube_collection_projection_guard_singleton CHECK (guard_key)
);

INSERT INTO public.youtube_collection_projection_guard (guard_key)
VALUES (TRUE)
ON CONFLICT (guard_key) DO NOTHING;

-- 첫 statement가 guard를 공유 잠금으로 기다린 뒤, 다음 statement가 새 snapshot으로 CURRENT를 읽는다.
-- READ COMMITTED 호출자를 전제로 하며, guard가 없으면 잠금 없이 진행하지 않고 실패한다.
CREATE OR REPLACE FUNCTION public.lock_current_youtube_collection_projection()
RETURNS TABLE (generation BIGINT)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog
AS $function$
#variable_conflict use_column
BEGIN
    PERFORM 1
    FROM public.youtube_collection_projection_guard AS guard
    WHERE guard.guard_key
    FOR SHARE OF guard;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'youtube collection projection guard row is missing'
            USING ERRCODE = 'P0002';
    END IF;

    RETURN QUERY
    SELECT projection.generation
    FROM public.youtube_collection_projection_generations AS projection
    WHERE projection.status = 'CURRENT'
      AND projection.valid_until > pg_catalog.clock_timestamp();
END
$function$;

-- job 유효성의 단일 규칙이다. 유효한 CURRENT의 target 중 lease 범위(kind 집합, exact이면 같은 subject)에
-- 드는 enabled·미만료 row 수가 acquire 때 기록한 0 아닌 수와 같고, 모든 row가 proof generation
-- 이하에서 이어진 membership이어야 한다. 삭제·disable은 수 감소로, cadence/priority 변경·재등록(ABA)은
-- 더 큰 member_since_generation으로 거부된다. RETIRED target과 과거 이력은 읽지 않는다.
-- STABLE이므로 호출 statement의 snapshot에서 평가된다.
CREATE OR REPLACE FUNCTION public.youtube_collection_membership_valid(
    requested_kinds TEXT[],
    requested_exact_subject BOOLEAN,
    requested_subject_key TEXT,
    requested_target_count INTEGER,
    requested_generation BIGINT
)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SET search_path = pg_catalog
AS $function$
    SELECT COALESCE(requested_target_count > 0, FALSE)
       AND COALESCE(cardinality(requested_kinds) > 0, FALSE)
       AND COALESCE(requested_generation > 0, FALSE)
       AND requested_exact_subject IS NOT NULL
       AND EXISTS (
           SELECT 1
           FROM public.youtube_collection_projection_generations AS projection
           CROSS JOIN LATERAL (
               SELECT count(target.subject_key) AS target_count,
                      count(target.subject_key) FILTER (
                          WHERE target.member_since_generation BETWEEN 1 AND requested_generation
                      ) AS member_count
               FROM public.youtube_collection_targets AS target
               WHERE target.projection_generation = projection.generation
                 AND target.observation_kind = ANY (requested_kinds)
                 AND target.enabled
                 AND target.valid_until > statement_timestamp()
                 AND (NOT requested_exact_subject OR target.subject_key = requested_subject_key)
           ) AS scope
           WHERE projection.status = 'CURRENT'
             AND projection.valid_until > statement_timestamp()
             AND scope.target_count = requested_target_count
             AND scope.member_count = requested_target_count
       )
$function$;

-- generation 번호를 미리 읽고 그 row만 잠그던 이전 함수는 CURRENT 전환 대기 뒤 0행을 받는다.
-- 새 collector는 lock_current_youtube_collection_projection()만 쓰며, 혼합 배포 호환 경로는 두지 않는다.
DROP FUNCTION IF EXISTS public.lock_youtube_collection_projection(BIGINT);

-- 두 잠금 함수는 UNIQUE identity/digest 전체 키를 조회하므로 0 또는 1행만 반환한다.
-- 기본 SRF 추정 1,000행은 publish의 단일 입력을 100,000행으로 부풀려 수락 간격 JOIN에서
-- 매번 약 90ms의 JIT를 유발했다. 실제 cardinality를 알리며 JIT·성능 예산·잠금 의미는 바꾸지 않는다.
ALTER FUNCTION public.lock_source_observation_identity(TEXT, TEXT, TEXT, TEXT, SMALLINT, BIGINT) ROWS 1;
ALTER FUNCTION public.lock_source_observation_payload(TEXT, SMALLINT, BYTEA, JSONB) ROWS 1;

REVOKE ALL ON TABLE public.youtube_collection_projection_guard FROM PUBLIC;
REVOKE ALL ON FUNCTION public.lock_current_youtube_collection_projection() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.youtube_collection_membership_valid(TEXT[], BOOLEAN, TEXT, INTEGER, BIGINT) FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        -- refresh의 FOR UPDATE에는 UPDATE 권한이 필요하다. row 삭제·추가 권한은 주지 않는다.
        GRANT SELECT, UPDATE ON TABLE public.youtube_collection_projection_guard TO hololive_runtime;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT EXECUTE ON FUNCTION public.lock_current_youtube_collection_projection() TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.youtube_collection_membership_valid(TEXT[], BOOLEAN, TEXT, INTEGER, BIGINT)
            TO hololive_scraper;
    END IF;
END
$migration$;

COMMIT;
