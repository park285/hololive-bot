-- 구조 집계만 원본 쓰기와 함께 확정한다. CURRENT 유효기간·lease·현재 시각은 소비자가 판정한다.
BEGIN;
SET LOCAL lock_timeout = '3s';

ALTER TABLE public.youtube_collection_projection_guard
    ADD COLUMN IF NOT EXISTS runtime_writer xid8;

CREATE OR REPLACE FUNCTION public.lock_youtube_collection_runtime_bundles()
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $lock$
BEGIN
    -- 기존 collector의 SHARE/UPDATE 잠금과 같은 행을 쓴다. 실제 버전 변경으로 오래된 RR snapshot도 거부한다.
    -- 같은 트랜잭션의 재진입은 행을 다시 쓰지 않는다. 기존 singleton 누락은 복구하지 않고 거부한다.
    UPDATE public.youtube_collection_projection_guard AS guard
    SET runtime_writer = pg_catalog.pg_current_xact_id()
    WHERE guard.guard_key AND guard.runtime_writer IS DISTINCT FROM pg_catalog.pg_current_xact_id();
    IF NOT FOUND AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_projection_guard WHERE guard_key) THEN
        RAISE EXCEPTION 'youtube collection projection guard is missing';
    END IF;
END
$lock$;
REVOKE ALL ON FUNCTION public.lock_youtube_collection_runtime_bundles() FROM PUBLIC;

SELECT public.lock_youtube_collection_runtime_bundles();
-- 설치·재적용·backfill을 한 배제 구간에 넣어 trigger 설치 전 원본 쓰기를 놓치지 않는다.
LOCK TABLE public.youtube_collection_projection_generations,
    public.youtube_collection_targets IN SHARE ROW EXCLUSIVE MODE;

CREATE OR REPLACE VIEW public.youtube_collection_runtime_kinds (kind, observation_kind, rpc_per_kind) AS
VALUES
    ('youtubejs_channel_live'::text, 'live_snapshot'::text, false),
    ('youtubejs_channel_live_check', 'channel_live_check', false),
    ('youtubejs_video_live', 'video_live_check', false),
    ('community_collect', 'community_page', false),
    ('youtubejs_content', 'video_list', true),
    ('youtubejs_content', 'shorts_list', true),
    ('youtubejs_channel_metadata', 'channel_profile', false),
    ('youtubejs_channel_metadata', 'channel_photo', false);

CREATE TABLE IF NOT EXISTS public.youtube_collection_runtime_bundles (
    projection_generation bigint NOT NULL,
    kind text COLLATE "C" NOT NULL,
    subject_key text COLLATE "C" NOT NULL,
    rpc_calls bigint NOT NULL,
    interval_ms bigint NOT NULL,
    created_at timestamptz NOT NULL,
    eligible_at timestamptz NOT NULL,
    PRIMARY KEY (projection_generation, kind, subject_key)
);
COMMENT ON TABLE public.youtube_collection_runtime_bundles IS
    'CURRENT/STAGING의 enabled target을 bytewise bundle로 집계한 동기 모델. 유효기간과 lease는 복사하지 않는다. '
    'writer는 builder/header/target 잠금 전에 기존 projection guard를 lock_youtube_collection_runtime_bundles()로 획득한다.';
REVOKE ALL ON TABLE public.youtube_collection_runtime_kinds, public.youtube_collection_runtime_bundles FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.refresh_youtube_collection_runtime_bundle_keys(
    p_generations bigint[], p_kinds text[], p_subjects text[]
)
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $refresh$
BEGIN
    PERFORM public.lock_youtube_collection_runtime_bundles();
    -- 내부 호출자는 세 배열을 같은 key 순서로 만든다. VOLATILE의 새 문장이 잠금 대기 뒤 커밋도 읽는다.
    WITH requested AS MATERIALIZED (
        SELECT projection_generation, kind COLLATE "C" AS kind, subject_key COLLATE "C" AS subject_key
        FROM ROWS FROM (pg_catalog.unnest(p_generations), pg_catalog.unnest(p_kinds), pg_catalog.unnest(p_subjects))
            AS keys(projection_generation, kind, subject_key)
    ), fresh AS MATERIALIZED (
        SELECT keys.projection_generation, keys.kind, keys.subject_key,
               CASE WHEN mapping.rpc_per_kind THEN count(target.observation_kind) ELSE 1 END AS rpc_calls,
               min(target.poll_interval_ms) AS interval_ms, min(target.created_at) AS created_at,
               min(COALESCE(target.not_before, '-infinity'::timestamptz)) AS eligible_at
        FROM requested AS keys
        JOIN public.youtube_collection_projection_generations AS generation
          ON generation.generation = keys.projection_generation AND generation.status IN ('CURRENT', 'STAGING')
        JOIN public.youtube_collection_runtime_kinds AS mapping ON mapping.kind COLLATE "C" = keys.kind
        JOIN public.youtube_collection_targets AS target
          ON target.projection_generation = keys.projection_generation
         -- 원본 PK의 기본 collation index를 쓰되 최종 identity는 반드시 bytewise로 확인한다.
         AND target.subject_key = keys.subject_key COLLATE "default"
         AND target.subject_key COLLATE "C" = keys.subject_key
         AND target.observation_kind = mapping.observation_kind
         AND target.enabled
        GROUP BY keys.projection_generation, keys.kind, keys.subject_key, mapping.rpc_per_kind
    ), removed AS (
        DELETE FROM public.youtube_collection_runtime_bundles AS bundle
        USING requested AS keys
        WHERE (bundle.projection_generation, bundle.kind, bundle.subject_key) =
              (keys.projection_generation, keys.kind, keys.subject_key)
          AND NOT EXISTS (
              SELECT 1 FROM fresh
              WHERE (fresh.projection_generation, fresh.kind, fresh.subject_key) =
                    (bundle.projection_generation, bundle.kind, bundle.subject_key)
          )
    )
    INSERT INTO public.youtube_collection_runtime_bundles AS bundle
        (projection_generation, kind, subject_key, rpc_calls, interval_ms, created_at, eligible_at)
    SELECT projection_generation, kind, subject_key, rpc_calls, interval_ms, created_at, eligible_at
    FROM fresh
    ORDER BY projection_generation, kind, subject_key
    ON CONFLICT (projection_generation, kind, subject_key) DO UPDATE SET
        rpc_calls = EXCLUDED.rpc_calls, interval_ms = EXCLUDED.interval_ms,
        created_at = EXCLUDED.created_at, eligible_at = EXCLUDED.eligible_at
    WHERE bundle IS DISTINCT FROM EXCLUDED;
END
$refresh$;
REVOKE ALL ON FUNCTION public.refresh_youtube_collection_runtime_bundle_keys(bigint[], text[], text[]) FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.refresh_youtube_collection_runtime_bundles(p_generations bigint[])
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $refresh_generations$
DECLARE
    generations bigint[];
    kinds text[];
    subjects text[];
BEGIN
    PERFORM public.lock_youtube_collection_runtime_bundles();
    -- header 전환만 generation 전체를 갱신한다. RETIRED 원본 이력은 읽지 않고 기존 파생 key만 제거한다.
    WITH keys AS (
        SELECT bundle.projection_generation, bundle.kind, bundle.subject_key
        FROM public.youtube_collection_runtime_bundles AS bundle
        WHERE bundle.projection_generation = ANY(p_generations)
        UNION
        SELECT target.projection_generation, mapping.kind COLLATE "C", target.subject_key COLLATE "C"
        FROM public.youtube_collection_projection_generations AS generation
        JOIN public.youtube_collection_targets AS target ON target.projection_generation = generation.generation
        JOIN public.youtube_collection_runtime_kinds AS mapping ON mapping.observation_kind = target.observation_kind
        WHERE generation.generation = ANY(p_generations)
          AND generation.status IN ('CURRENT', 'STAGING') AND target.enabled
    )
    SELECT array_agg(projection_generation ORDER BY projection_generation, kind, subject_key),
           array_agg(kind ORDER BY projection_generation, kind, subject_key),
           array_agg(subject_key ORDER BY projection_generation, kind, subject_key)
    INTO generations, kinds, subjects FROM keys;
    IF generations IS NOT NULL THEN
        PERFORM public.refresh_youtube_collection_runtime_bundle_keys(generations, kinds, subjects);
    END IF;
END
$refresh_generations$;
REVOKE ALL ON FUNCTION public.refresh_youtube_collection_runtime_bundles(bigint[]) FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.guard_youtube_collection_runtime_bundles()
RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $guard$
BEGIN
    PERFORM public.lock_youtube_collection_runtime_bundles();
    RETURN NULL;
END
$guard$;
REVOKE ALL ON FUNCTION public.guard_youtube_collection_runtime_bundles() FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement()
RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $statement$
DECLARE
    generations bigint[];
    kinds text[];
    subjects text[];
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        -- 두 원본 중 어느 쪽이 비어도 유효 bundle은 없다. TRUNCATE에는 transition table이 없다.
        DELETE FROM public.youtube_collection_runtime_bundles;
        RETURN NULL;
    END IF;

    IF TG_TABLE_NAME = 'youtube_collection_projection_generations' THEN
        IF TG_OP = 'INSERT' THEN
            SELECT array_agg(generation ORDER BY generation) INTO generations
            FROM runtime_new WHERE status IN ('CURRENT', 'STAGING');
        ELSIF TG_OP = 'DELETE' THEN
            SELECT array_agg(generation ORDER BY generation) INTO generations FROM runtime_old;
        ELSE
            -- TTL·hash·eligibility heartbeat는 구조 집계를 건드리지 않는다. PK 이동은 양쪽을 포함한다.
            WITH changed AS MATERIALIZED (
                SELECT prior.generation AS old_generation, next.generation AS new_generation
                FROM runtime_old AS prior FULL JOIN runtime_new AS next USING (generation)
                WHERE prior.generation IS NULL OR next.generation IS NULL
                   OR (prior.status IN ('CURRENT', 'STAGING')) IS DISTINCT FROM (next.status IN ('CURRENT', 'STAGING'))
            )
            SELECT array_agg(generation ORDER BY generation) INTO generations
            FROM (SELECT old_generation AS generation FROM changed WHERE old_generation IS NOT NULL
                  UNION SELECT new_generation FROM changed WHERE new_generation IS NOT NULL) AS keys;
        END IF;
        IF generations IS NOT NULL THEN
            PERFORM public.refresh_youtube_collection_runtime_bundles(generations);
        END IF;
        RETURN NULL;
    END IF;

    -- target 한 행의 변경으로 generation 전체나 RETIRED 이력을 집계하지 않는다.
    IF TG_OP = 'INSERT' THEN
        WITH keys AS (
            SELECT DISTINCT target.projection_generation, mapping.kind COLLATE "C" AS kind,
                   target.subject_key COLLATE "C" AS subject_key
            FROM runtime_new AS target JOIN public.youtube_collection_runtime_kinds AS mapping
              ON mapping.observation_kind = target.observation_kind
        )
        SELECT array_agg(projection_generation ORDER BY projection_generation, kind, subject_key),
               array_agg(kind ORDER BY projection_generation, kind, subject_key),
               array_agg(subject_key ORDER BY projection_generation, kind, subject_key)
        INTO generations, kinds, subjects FROM keys;
    ELSIF TG_OP = 'DELETE' THEN
        WITH keys AS (
            SELECT DISTINCT target.projection_generation, mapping.kind COLLATE "C" AS kind,
                   target.subject_key COLLATE "C" AS subject_key
            FROM runtime_old AS target JOIN public.youtube_collection_runtime_kinds AS mapping
              ON mapping.observation_kind = target.observation_kind
        )
        SELECT array_agg(projection_generation ORDER BY projection_generation, kind, subject_key),
               array_agg(kind ORDER BY projection_generation, kind, subject_key),
               array_agg(subject_key ORDER BY projection_generation, kind, subject_key)
        INTO generations, kinds, subjects FROM keys;
    ELSE
        WITH changed AS MATERIALIZED (
            SELECT prior.projection_generation AS old_generation, prior.subject_key AS old_subject,
                   prior.observation_kind AS old_observation, next.projection_generation AS new_generation,
                   next.subject_key AS new_subject, next.observation_kind AS new_observation
            FROM runtime_old AS prior FULL JOIN runtime_new AS next
              ON prior.projection_generation = next.projection_generation
             AND prior.subject_key COLLATE "C" = next.subject_key COLLATE "C"
             AND prior.observation_kind COLLATE "C" = next.observation_kind COLLATE "C"
            WHERE prior.projection_generation IS NULL OR next.projection_generation IS NULL
               OR (prior.enabled, prior.poll_interval_ms, prior.created_at, prior.not_before)
                  IS DISTINCT FROM (next.enabled, next.poll_interval_ms, next.created_at, next.not_before)
        ), keys AS (
            SELECT DISTINCT target.projection_generation, mapping.kind COLLATE "C" AS kind,
                   target.subject_key COLLATE "C" AS subject_key
            FROM changed CROSS JOIN LATERAL (
                VALUES (old_generation, old_subject, old_observation),
                       (new_generation, new_subject, new_observation)
            ) AS target(projection_generation, subject_key, observation_kind)
            JOIN public.youtube_collection_runtime_kinds AS mapping ON mapping.observation_kind = target.observation_kind
            WHERE target.projection_generation IS NOT NULL
        )
        SELECT array_agg(projection_generation ORDER BY projection_generation, kind, subject_key),
               array_agg(kind ORDER BY projection_generation, kind, subject_key),
               array_agg(subject_key ORDER BY projection_generation, kind, subject_key)
        INTO generations, kinds, subjects FROM keys;
    END IF;
    IF generations IS NOT NULL THEN
        PERFORM public.refresh_youtube_collection_runtime_bundle_keys(generations, kinds, subjects);
    END IF;
    RETURN NULL;
END
$statement$;
REVOKE ALL ON FUNCTION public.refresh_youtube_collection_runtime_bundles_statement() FROM PUBLIC;

DO $triggers$
DECLARE
    source_table text;
BEGIN
    FOREACH source_table IN ARRAY ARRAY['youtube_collection_projection_generations', 'youtube_collection_targets']
    LOOP
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_collection_runtime_guard BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.%I FOR EACH STATEMENT EXECUTE FUNCTION public.guard_youtube_collection_runtime_bundles()', source_table);
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_collection_runtime_insert AFTER INSERT ON public.%I REFERENCING NEW TABLE AS runtime_new FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement()', source_table);
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_collection_runtime_update AFTER UPDATE ON public.%I REFERENCING OLD TABLE AS runtime_old NEW TABLE AS runtime_new FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement()', source_table);
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_collection_runtime_delete AFTER DELETE ON public.%I REFERENCING OLD TABLE AS runtime_old FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement()', source_table);
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_collection_runtime_truncate AFTER TRUNCATE ON public.%I FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement()', source_table);
    END LOOP;
END
$triggers$;

-- 기존 bounded retention의 검증·상한·SKIP LOCKED·FK 재확인·반환값은 유지하고 잠금 순서만 통일한다.
CREATE OR REPLACE FUNCTION public.delete_retired_youtube_projection_batch(
    requested_cutoff TIMESTAMPTZ,
    requested_limit INTEGER
)
RETURNS TABLE (deleted_reasons BIGINT, deleted_targets BIGINT, deleted_generations BIGINT)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    chosen_generation BIGINT;
    locked_target_ctids TID[];
BEGIN
    deleted_reasons := 0;
    deleted_targets := 0;
    deleted_generations := 0;
    IF requested_cutoff IS NULL OR requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'invalid projection retention request' USING ERRCODE = '22023';
    END IF;

    PERFORM public.lock_youtube_collection_runtime_bundles();

    -- generation의 UPDATE 잠금은 새 lease의 FK KEY SHARE와 충돌합니다.
    -- 잠금을 얻는 동안 commit된 lease가 있으면 새 문장의 snapshot으로 다시 확인하여
    -- 아직 참조되는 generation의 하위 데이터를 지우지 않습니다.
    SELECT generation.generation INTO chosen_generation
    FROM public.youtube_collection_projection_generations AS generation
    WHERE generation.status = 'RETIRED'
      AND generation.valid_until < requested_cutoff
      AND NOT EXISTS (
          SELECT 1 FROM public.youtube_collection_job_leases AS lease
          WHERE lease.projection_generation = generation.generation
      )
    ORDER BY generation.valid_until, generation.generation
    LIMIT 1
    FOR UPDATE OF generation SKIP LOCKED;

    IF chosen_generation IS NULL OR EXISTS (
        SELECT 1 FROM public.youtube_collection_job_leases AS lease
        WHERE lease.projection_generation = chosen_generation
    ) THEN
        RETURN NEXT;
        RETURN;
    END IF;

    WITH candidates AS (
        SELECT reason.projection_generation, reason.subject_key,
               reason.observation_kind, reason.reason_kind, reason.reason_key
        FROM public.youtube_collection_target_reasons AS reason
        WHERE reason.projection_generation = chosen_generation
        ORDER BY reason.subject_key, reason.observation_kind,
                 reason.reason_kind, reason.reason_key
        LIMIT requested_limit
        FOR UPDATE OF reason SKIP LOCKED
    )
    DELETE FROM public.youtube_collection_target_reasons AS reason
    USING candidates AS candidate
    WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind,
           reason.reason_kind, reason.reason_key) =
          (candidate.projection_generation, candidate.subject_key, candidate.observation_kind,
           candidate.reason_kind, candidate.reason_key);
    GET DIAGNOSTICS deleted_reasons = ROW_COUNT;

    -- target을 먼저 제한된 수만큼 잠급니다. reason INSERT의 FK KEY SHARE와 충돌하므로,
    -- 잠금을 기다리는 동안 추가된 reason까지 새 문장의 snapshot으로 확인할 수 있습니다.
    -- 자식이 없는 target만 지워 FK cascade가 배치 상한을 우회하지 않게 합니다.
    IF deleted_reasons < requested_limit THEN
        SELECT pg_catalog.array_agg(candidate.ctid) INTO locked_target_ctids
        FROM (
            SELECT target.ctid
            FROM public.youtube_collection_targets AS target
            WHERE target.projection_generation = chosen_generation
              AND NOT EXISTS (
                  SELECT 1 FROM public.youtube_collection_target_reasons AS reason
                  WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind) =
                        (target.projection_generation, target.subject_key, target.observation_kind)
              )
            ORDER BY target.subject_key, target.observation_kind
            LIMIT requested_limit - deleted_reasons
            FOR UPDATE OF target SKIP LOCKED
        ) AS candidate;

        DELETE FROM public.youtube_collection_targets AS target
        WHERE target.projection_generation = chosen_generation
          AND target.ctid = ANY(locked_target_ctids)
          AND NOT EXISTS (
              SELECT 1 FROM public.youtube_collection_target_reasons AS reason
              WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind) =
                    (target.projection_generation, target.subject_key, target.observation_kind)
          );
        GET DIAGNOSTICS deleted_targets = ROW_COUNT;
    END IF;

    DELETE FROM public.youtube_collection_projection_generations AS generation
    WHERE generation.generation = chosen_generation
      AND generation.status = 'RETIRED'
      AND generation.valid_until < requested_cutoff
      AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_job_leases AS lease
                      WHERE lease.projection_generation = generation.generation)
      AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_targets AS target
                      WHERE target.projection_generation = generation.generation);
    GET DIAGNOSTICS deleted_generations = ROW_COUNT;
    RETURN NEXT;
END
$$;
-- CREATE OR REPLACE가 기존 retention ACL을 보존한다. 새 내부 함수의 default EXECUTE는 따로 회수한다.
DO $roles$
DECLARE
    guard_role text;
BEGIN
    -- 기존 guard_key 쓰기·FOR UPDATE 권한은 유지하되 새 xid8 컬럼에는 확장하지 않는다.
    FOR guard_role IN SELECT rolname FROM pg_catalog.pg_roles
        WHERE rolname IN ('hololive_runtime', 'hololive_scraper')
    LOOP
        IF pg_catalog.has_table_privilege(guard_role, 'public.youtube_collection_projection_guard', 'UPDATE') THEN
            EXECUTE format('REVOKE UPDATE ON TABLE public.youtube_collection_projection_guard FROM %I', guard_role);
            EXECUTE format('GRANT UPDATE (guard_key) ON TABLE public.youtube_collection_projection_guard TO %I', guard_role);
        END IF;
        IF pg_catalog.has_table_privilege(guard_role, 'public.youtube_collection_projection_guard', 'INSERT') THEN
            EXECUTE format('REVOKE INSERT ON TABLE public.youtube_collection_projection_guard FROM %I', guard_role);
            EXECUTE format('GRANT INSERT (guard_key) ON TABLE public.youtube_collection_projection_guard TO %I', guard_role);
        END IF;
    END LOOP;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE public.youtube_collection_runtime_kinds, public.youtube_collection_runtime_bundles FROM hololive_runtime;
        GRANT SELECT ON TABLE public.youtube_collection_runtime_kinds, public.youtube_collection_runtime_bundles TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.lock_youtube_collection_runtime_bundles() TO hololive_runtime;
        REVOKE ALL ON FUNCTION public.refresh_youtube_collection_runtime_bundle_keys(bigint[], text[], text[]),
            public.refresh_youtube_collection_runtime_bundles(bigint[]), public.guard_youtube_collection_runtime_bundles(),
            public.refresh_youtube_collection_runtime_bundles_statement() FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE public.youtube_collection_runtime_kinds, public.youtube_collection_runtime_bundles FROM hololive_scraper;
        REVOKE ALL ON FUNCTION public.lock_youtube_collection_runtime_bundles(),
            public.refresh_youtube_collection_runtime_bundle_keys(bigint[], text[], text[]),
            public.refresh_youtube_collection_runtime_bundles(bigint[]), public.guard_youtube_collection_runtime_bundles(),
            public.refresh_youtube_collection_runtime_bundles_statement() FROM hololive_scraper;
    END IF;
END
$roles$;

-- 재적용 때 남은 파생 key도 포함하되 원본 RETIRED 이력은 backfill하지 않는다.
SELECT public.refresh_youtube_collection_runtime_bundles(ARRAY(
    SELECT generation FROM public.youtube_collection_projection_generations WHERE status IN ('CURRENT', 'STAGING')
    UNION SELECT projection_generation FROM public.youtube_collection_runtime_bundles
));
COMMIT;
