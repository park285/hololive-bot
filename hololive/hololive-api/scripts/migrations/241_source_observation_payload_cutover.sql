BEGIN;
SET LOCAL lock_timeout = '3s';
-- 빈 DB는 바로 전환합니다. 데이터가 있으면 writer 정지 후 독립 commit의
-- bounded backfill을 먼저 완료해야 합니다. 여기서는 마지막 최대 1000행만 처리합니다.
DO $cutover$
BEGIN
    IF to_regprocedure('public.backfill_source_observation_payloads(integer)') IS NOT NULL THEN
        IF (SELECT count(indexrelid) FROM pg_catalog.pg_index
            WHERE indexrelid IN ('public.idx_source_observations_payload_id'::regclass,
                                 'public.idx_source_observations_payload_backfill'::regclass)
              AND indisvalid AND indisready) <> 2 THEN
            RAISE EXCEPTION 'payload indexes are invalid; rebuild the failed concurrent index before resuming';
        END IF;
        PERFORM public.backfill_source_observation_payloads(1000);
    END IF;
    IF EXISTS (SELECT 1 FROM public.source_observations WHERE payload_id IS NULL LIMIT 1) THEN
        RAISE EXCEPTION 'source observation payload backfill incomplete; quiesce writers, complete bounded batches, then retry migration 241';
    END IF;
END
$cutover$;

ALTER TABLE public.source_observations
    DROP CONSTRAINT IF EXISTS fk_source_observation_payload;
ALTER TABLE public.source_observations
    ADD CONSTRAINT fk_source_observation_payload
    FOREIGN KEY (payload_id) REFERENCES public.source_observation_payloads(id)
    ON DELETE RESTRICT NOT VALID;
ALTER TABLE public.source_observations
    VALIDATE CONSTRAINT fk_source_observation_payload;
ALTER TABLE public.source_observations
    DROP CONSTRAINT IF EXISTS chk_source_observation_payload_id_present;
ALTER TABLE public.source_observations
    ADD CONSTRAINT chk_source_observation_payload_id_present
    CHECK (payload_id IS NOT NULL) NOT VALID;
ALTER TABLE public.source_observations
    VALIDATE CONSTRAINT chk_source_observation_payload_id_present;
ALTER TABLE public.source_observations ALTER COLUMN payload_id SET NOT NULL;
ALTER TABLE public.source_observations DROP CONSTRAINT chk_source_observation_payload_id_present;
ALTER TABLE public.source_observations DROP CONSTRAINT IF EXISTS chk_source_observation_payload;
ALTER TABLE public.source_observations DROP CONSTRAINT IF EXISTS chk_source_observation_hashes;
ALTER TABLE public.source_observations ADD CONSTRAINT chk_source_observation_hashes
    CHECK (scope_sha256 ~ '^[0-9a-f]{64}$' AND evidence_sha256 ~ '^[0-9a-f]{64}$') NOT VALID;
ALTER TABLE public.source_observations VALIDATE CONSTRAINT chk_source_observation_hashes;
ALTER TABLE public.source_observations DROP COLUMN IF EXISTS payload;
ALTER TABLE public.source_observations DROP COLUMN IF EXISTS payload_sha256;
DROP FUNCTION IF EXISTS public.backfill_source_observation_payloads(integer);

CREATE TABLE IF NOT EXISTS public.source_observation_payload_gc_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    cursor_id bigint NOT NULL DEFAULT 0 CHECK (cursor_id >= 0)
);
INSERT INTO public.source_observation_payload_gc_state (singleton) VALUES (true)
ON CONFLICT (singleton) DO NOTHING;

-- VOLATILE is essential: under READ COMMITTED the identity/advisory locks may
-- have waited on a competing publisher or GC after the outer statement snapshot.
-- This inner statement must see that transaction's committed dictionary row.
CREATE OR REPLACE FUNCTION public.lock_source_observation_payload(
    requested_kind text, requested_version smallint, requested_digest bytea,
    expected_payload jsonb
) RETURNS TABLE(id bigint)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
DECLARE
    stored_payload jsonb;
BEGIN
    SELECT candidate.id, candidate.payload
    INTO id, stored_payload
    FROM public.source_observation_payloads AS candidate
    WHERE candidate.observation_kind = requested_kind
      AND candidate.schema_version = requested_version
      AND candidate.canonical_profile = 'source-observation-canonical-json-v1'
      AND candidate.payload_sha256 = requested_digest
    FOR KEY SHARE OF candidate;
    IF FOUND THEN
        IF stored_payload IS DISTINCT FROM expected_payload THEN
            RAISE EXCEPTION 'source observation payload digest collision or content mismatch';
        END IF;
        RETURN NEXT;
    END IF;
END
$function$;
REVOKE ALL ON FUNCTION public.lock_source_observation_payload(text, smallint, bytea, jsonb) FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.assert_source_observation_payload_match(
    requested_id bigint, stored_payload jsonb, expected_payload jsonb
) RETURNS bigint
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
BEGIN
    IF stored_payload IS DISTINCT FROM expected_payload THEN
        RAISE EXCEPTION 'source observation payload digest collision or content mismatch';
    END IF;
    RETURN requested_id;
END
$function$;
REVOKE ALL ON FUNCTION public.assert_source_observation_payload_match(bigint, jsonb, jsonb) FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.assert_source_observation_payload_count(actual bigint, expected bigint)
RETURNS bigint
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
BEGIN
    IF actual <> expected THEN
        RAISE EXCEPTION 'source observation payload resolution incomplete: % of %', actual, expected;
    END IF;
    RETURN actual;
END
$function$;
REVOKE ALL ON FUNCTION public.assert_source_observation_payload_count(bigint, bigint) FROM PUBLIC;

-- A missing/corrupt reference is never interpreted as a missing observation.
-- The caller's existing JSON/hash contract is reconstructed in the same SELECT.
CREATE OR REPLACE FUNCTION public.require_source_observation_payload(
    requested_id bigint, requested_kind text, requested_version smallint,
    actual_kind text, actual_version smallint, actual_profile text,
    actual_digest bytea, actual_payload jsonb
) RETURNS jsonb
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
BEGIN
    IF actual_payload IS NULL OR actual_kind IS DISTINCT FROM requested_kind
       OR actual_version IS DISTINCT FROM requested_version
       OR actual_profile IS DISTINCT FROM 'source-observation-canonical-json-v1'
       OR actual_digest IS NULL OR octet_length(actual_digest) <> 32 THEN
        RAISE EXCEPTION 'missing or corrupt source observation payload for observation %', requested_id;
    END IF;
    RETURN actual_payload;
END
$function$;
REVOKE ALL ON FUNCTION public.require_source_observation_payload(bigint, text, smallint, text, smallint, text, bytea, jsonb) FROM PUBLIC;


-- 행 잠금 후 새 statement snapshot으로 참조를 확인합니다. 잠금을 얻기 직전에
-- commit된 publisher의 참조를 놓치지 않으며 runtime에 payload UPDATE 권한을 주지 않습니다.
CREATE OR REPLACE FUNCTION public.delete_source_observation_payload_batch(
    requested_cutoff timestamptz, requested_limit integer
) RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
DECLARE
    previous_id bigint;
    candidate_ids bigint[];
    deleted bigint;
BEGIN
    IF requested_cutoff IS NULL OR requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'invalid payload retention request' USING ERRCODE = '22023';
    END IF;
    SELECT cursor_id INTO previous_id FROM public.source_observation_payload_gc_state
    WHERE singleton FOR UPDATE;
    SELECT array_agg(candidate.id ORDER BY candidate.id) INTO candidate_ids
    FROM (
        SELECT payload.id FROM public.source_observation_payloads payload
        WHERE payload.id > previous_id AND payload.created_at < requested_cutoff
        ORDER BY payload.id LIMIT requested_limit FOR UPDATE SKIP LOCKED
    ) candidate;
    DELETE FROM public.source_observation_payloads payload
    WHERE payload.id = ANY(candidate_ids)
      AND NOT EXISTS (SELECT 1 FROM public.source_observations observation WHERE observation.payload_id = payload.id);
    GET DIAGNOSTICS deleted = ROW_COUNT;
    UPDATE public.source_observation_payload_gc_state
    SET cursor_id = coalesce(candidate_ids[array_length(candidate_ids, 1)], 0)
    WHERE singleton;
    RETURN deleted;
END
$function$;
REVOKE ALL ON FUNCTION public.delete_source_observation_payload_batch(timestamptz, integer) FROM PUBLIC;

DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT, INSERT ON public.source_observation_payloads TO hololive_scraper;
        GRANT USAGE, SELECT ON SEQUENCE public.source_observation_payloads_id_seq TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.lock_source_observation_payload(text, smallint, bytea, jsonb) TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.assert_source_observation_payload_match(bigint, jsonb, jsonb) TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.assert_source_observation_payload_count(bigint, bigint) TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.lock_source_observation_identity(text, text, text, text, smallint, bigint) TO hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON public.source_observation_payloads TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.delete_source_observation_payload_batch(timestamptz, integer) TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.require_source_observation_payload(bigint, text, smallint, text, smallint, text, bytea, jsonb) TO hololive_runtime;
    END IF;
END
$roles$;
COMMIT;
