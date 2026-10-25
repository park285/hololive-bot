BEGIN;
SET LOCAL lock_timeout = '3s';

-- 315의 창 하나 처리를 진척 결과(삭제 id, 커서가 지나간 후보 수, 순회 완료)를 돌려주는 함수로 옮긴다.
-- runtime은 커서 상태 표를 읽을 수 없으므로, 한 tick에서 삭제 0건인 보호 창을 지나 다음 창을 처리할지 이 결과로 정한다.
-- 잠금 순서·창 크기·보호 재검사·커서 규칙은 315와 같다.
CREATE OR REPLACE FUNCTION public.delete_source_observation_retention_window(
    requested_kinds text[], requested_cutoffs timestamptz[], requested_limit integer,
    OUT deleted_ids bigint[], OUT passed integer, OUT traversal_completed boolean
)
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
    deleted_ids := '{}';
    passed := 0;
    traversal_completed := false;

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
    WITH deleted AS (
        DELETE FROM public.source_observations observation
        WHERE observation.id = ANY(locked_ids)
          AND NOT public.source_observation_retention_protected(observation.id)
        RETURNING observation.id
    )
    SELECT coalesce(array_agg(deleted.id), '{}') INTO deleted_ids FROM deleted;

    -- 삭제 상한을 채운 창은 마지막 잠근 행 다음부터, 아니면 창 끝 다음부터 이어 간다.
    -- passed는 다음 창이 다시 보지 않는 후보 수이며 삭제된 후보를 포함한다.
    IF locked = requested_limit THEN
        previous_at := locked_times[locked];
        previous_id := locked_ids[locked];
        passed := array_position(candidate_ids, previous_id);
    ELSE
        previous_at := candidate_times[scanned];
        previous_id := candidate_ids[scanned];
        passed := scanned;
    END IF;
    IF scanned = 0 OR (scanned < scan_limit AND locked < requested_limit)
       OR (previous_at, previous_id) >= (upper_at, high_id) THEN
        previous_at := '-infinity';
        previous_id := 0;
        upper_at := NULL;
        high_id := NULL;
        traversal_completed := true;
    END IF;
    UPDATE public.source_observation_retention_state state
    SET cursor_received_at = previous_at, cursor_id = previous_id,
        upper_received_at = upper_at, upper_id = high_id
    WHERE singleton;
END
$function$;
REVOKE ALL ON FUNCTION public.delete_source_observation_retention_window(text[], timestamptz[], integer) FROM PUBLIC;
DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.delete_source_observation_retention_window(text[], timestamptz[], integer) TO hololive_runtime;
    END IF;
END
$roles$;

-- 이전 image의 caller가 쓰는 공개 함수는 반환 계약(삭제된 id 행)을 유지하고 같은 창 처리를 위임한다.
-- 권한은 CREATE OR REPLACE가 보존한다.
CREATE OR REPLACE FUNCTION public.delete_source_observation_retention_batch(
    requested_kinds text[], requested_cutoffs timestamptz[], requested_limit integer
) RETURNS TABLE (deleted_id bigint)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog SET jit = off
AS $function$
BEGIN
    RETURN QUERY SELECT unnest(retention_window.deleted_ids)
    FROM public.delete_source_observation_retention_window(requested_kinds, requested_cutoffs, requested_limit) retention_window;
END
$function$;
COMMIT;
