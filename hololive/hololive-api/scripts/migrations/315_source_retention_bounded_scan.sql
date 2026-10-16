BEGIN;
SET LOCAL lock_timeout = '3s';

-- 커서와 원본 삭제는 함께 커밋한다. 프로세스 재시작은 진행을 잃지 않고 롤백은 둘 다 되돌린다.
CREATE TABLE IF NOT EXISTS public.source_observation_retention_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    cursor_received_at timestamptz NOT NULL DEFAULT '-infinity',
    cursor_id bigint NOT NULL DEFAULT 0 CHECK (cursor_id >= 0),
    upper_received_at timestamptz,
    upper_id bigint,
    CHECK ((upper_received_at IS NULL) = (upper_id IS NULL))
);
INSERT INTO public.source_observation_retention_state (singleton) VALUES (true)
ON CONFLICT (singleton) DO NOTHING;
REVOKE ALL ON TABLE public.source_observation_retention_state FROM PUBLIC;
DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE public.source_observation_retention_state FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE public.source_observation_retention_state FROM hololive_scraper;
    END IF;
END
$roles$;

CREATE OR REPLACE FUNCTION public.delete_source_observation_retention_batch(
    requested_kinds text[], requested_cutoffs timestamptz[], requested_limit integer
) RETURNS TABLE (deleted_id bigint)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog SET jit = off
AS $function$
DECLARE
    scan_limit CONSTANT integer := 1000;
    previous_at timestamptz;
    previous_id bigint;
    upper_at timestamptz;
    high_id bigint;
    candidate_ids bigint[];
    candidate_times timestamptz[];
    locked_ids bigint[];
    locked_times timestamptz[];
    scanned integer;
    locked integer;
BEGIN
    IF requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000
       OR cardinality(requested_kinds) IS NULL OR cardinality(requested_kinds) NOT BETWEEN 1 AND 16
       OR cardinality(requested_cutoffs) IS DISTINCT FROM cardinality(requested_kinds) THEN
        RETURN;
    END IF;

    -- 직접 SQL 호출도 live guard → 커서 → 원본 순서를 지킨다. 다음 문장이 대기 후 새 snapshot을 읽는다.
    PERFORM public.lock_youtube_live_runtime_state();
    SELECT state.cursor_received_at, state.cursor_id, state.upper_received_at, state.upper_id
    INTO STRICT previous_at, previous_id, upper_at, high_id
    FROM public.source_observation_retention_state state WHERE singleton FOR UPDATE;

    IF upper_at IS NULL THEN
        -- 순회 상한을 고정해 새 원본이 계속 들어와도 보호/잠금 해제된 앞쪽을 다시 방문한다.
        SELECT edge.received_at, edge.id INTO upper_at, high_id
        FROM (
            SELECT requested_kinds[p] AS kind, max(requested_cutoffs[p]) AS cutoff
            FROM generate_subscripts(requested_kinds, 1) p GROUP BY requested_kinds[p]
        ) policy CROSS JOIN LATERAL (
            SELECT observation.received_at, observation.id
            FROM public.source_observations observation
            WHERE observation.observation_kind = policy.kind AND observation.received_at < policy.cutoff
            ORDER BY observation.received_at DESC, observation.id DESC LIMIT 1
        ) edge ORDER BY edge.received_at DESC, edge.id DESC LIMIT 1;
    END IF;

    -- kind별 인덱스 탐색은 최대 16×1000행, 합친 후보는 최대 1000행이다.
    -- 보호 판정과 SKIP LOCKED는 이 창을 만든 뒤 실행하므로 보호/잠금 prefix를 무한히 훑지 않는다.
    WITH policies AS (
        SELECT requested_kinds[p] AS kind, max(requested_cutoffs[p]) AS cutoff
        FROM generate_subscripts(requested_kinds, 1) p GROUP BY requested_kinds[p]
    ), scan_window AS MATERIALIZED (
        SELECT candidate.id, candidate.received_at
        FROM policies policy CROSS JOIN LATERAL (
            SELECT observation.id, observation.received_at FROM public.source_observations observation
            WHERE observation.observation_kind = policy.kind AND observation.received_at < policy.cutoff
              AND (observation.received_at, observation.id) > (previous_at, previous_id)
              AND (observation.received_at, observation.id) <= (upper_at, high_id)
            ORDER BY observation.received_at, observation.id LIMIT scan_limit
        ) candidate ORDER BY candidate.received_at, candidate.id LIMIT scan_limit
    )
    SELECT array_agg(id ORDER BY received_at, id), array_agg(received_at ORDER BY received_at, id)
    INTO candidate_ids, candidate_times FROM scan_window;
    scanned := coalesce(cardinality(candidate_ids), 0);

    SELECT array_agg(candidate.id ORDER BY candidate.received_at, candidate.id),
           array_agg(candidate.received_at ORDER BY candidate.received_at, candidate.id)
    INTO locked_ids, locked_times
    FROM (
        SELECT observation.id, observation.received_at FROM public.source_observations observation
        WHERE observation.id = ANY(candidate_ids)
          AND NOT public.source_observation_retention_protected(observation.id)
        ORDER BY observation.received_at, observation.id
        LIMIT requested_limit FOR UPDATE OF observation SKIP LOCKED
    ) candidate;
    locked := coalesce(cardinality(locked_ids), 0);
    -- 행 잠금 획득 뒤 새 snapshot에서 보호를 재검사하고 FK CASCADE/SET NULL과 원자적으로 삭제한다.
    RETURN QUERY DELETE FROM public.source_observations observation
    WHERE observation.id = ANY(locked_ids)
      AND NOT public.source_observation_retention_protected(observation.id)
    RETURNING observation.id;

    IF locked = requested_limit THEN
        previous_at := locked_times[locked];
        previous_id := locked_ids[locked];
    ELSE
        previous_at := candidate_times[scanned];
        previous_id := candidate_ids[scanned];
    END IF;
    IF scanned = 0 OR (scanned < scan_limit AND locked < requested_limit)
       OR (previous_at, previous_id) >= (upper_at, high_id) THEN
        previous_at := '-infinity';
        previous_id := 0;
        upper_at := NULL;
        high_id := NULL;
    END IF;
    UPDATE public.source_observation_retention_state state
    SET cursor_received_at = previous_at, cursor_id = previous_id,
        upper_received_at = upper_at, upper_id = high_id
    WHERE singleton;
END
$function$;
REVOKE ALL ON FUNCTION public.delete_source_observation_retention_batch(text[], timestamptz[], integer) FROM PUBLIC;
COMMIT;
