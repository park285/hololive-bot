BEGIN;
SET LOCAL lock_timeout = '3s';

-- Registry 행은 보존된 관측과 reconciliation 기록의 FK 대상이다.
CREATE OR REPLACE FUNCTION public.reject_retired_holodex_channel_profile()
RETURNS trigger
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $function$
BEGIN
    RAISE EXCEPTION 'holodex channel_profile contract is retired'
        USING ERRCODE = '23514';
END
$function$;

CREATE OR REPLACE TRIGGER reject_retired_holodex_channel_profile
BEFORE INSERT ON public.source_observations
FOR EACH ROW WHEN (NEW.provider = 'holodex' AND NEW.observation_kind = 'channel_profile')
EXECUTE FUNCTION public.reject_retired_holodex_channel_profile();

CREATE OR REPLACE FUNCTION public.terminalize_retired_holodex_channel_profile()
RETURNS trigger
LANGUAGE plpgsql
SET search_path TO 'pg_catalog'
AS $function$
BEGIN
    IF EXISTS (
        SELECT 1 FROM public.source_observations AS observation
        WHERE observation.id = NEW.observation_id AND observation.provider = 'holodex'
          AND observation.observation_kind = 'channel_profile'
    ) THEN
        NEW.status := 'DEAD_LETTER';
        NEW.lease_owner := NULL;
        NEW.lease_token := NULL;
        NEW.lease_expires_at := NULL;
        NEW.processed_at := NULL;
        NEW.dead_lettered_at := pg_catalog.clock_timestamp();
        NEW.last_error_code := 'unsupported_contract';
        NEW.last_error_detail := 'holodex channel_profile contract is retired';
        NEW.updated_at := NEW.dead_lettered_at;
    END IF;
    RETURN NEW;
END
$function$;

CREATE OR REPLACE TRIGGER terminalize_retired_holodex_channel_profile
BEFORE INSERT OR UPDATE OF status, observation_id ON public.source_observation_queue
FOR EACH ROW WHEN (NEW.status IN ('PENDING', 'PROCESSING'))
EXECUTE FUNCTION public.terminalize_retired_holodex_channel_profile();
COMMIT;

DO $block$
DECLARE
    changed integer;
BEGIN
    LOOP
        PERFORM pg_catalog.set_config('lock_timeout', '3s', true);
        WITH batch AS (
            SELECT queue.observation_id
            FROM public.source_observation_queue AS queue
            JOIN public.source_observations AS observation ON observation.id = queue.observation_id
            WHERE observation.provider = 'holodex'
              AND observation.observation_kind = 'channel_profile'
              AND queue.status IN ('PENDING', 'PROCESSING')
            ORDER BY queue.observation_id
            LIMIT 1000
            FOR UPDATE OF queue
        )
        UPDATE public.source_observation_queue AS queue
        SET status = 'DEAD_LETTER', lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL,
            processed_at = NULL, dead_lettered_at = pg_catalog.clock_timestamp(),
            last_error_code = 'unsupported_contract',
            last_error_detail = 'holodex channel_profile contract is retired',
            updated_at = pg_catalog.clock_timestamp()
        FROM batch WHERE queue.observation_id = batch.observation_id;
        GET DIAGNOSTICS changed = ROW_COUNT;
        COMMIT;
        EXIT WHEN changed = 0;
    END LOOP;
END
$block$;
