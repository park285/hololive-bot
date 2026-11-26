BEGIN;
SET LOCAL lock_timeout = '3s';

LOCK TABLE public.source_observation_applications IN SHARE MODE;

CREATE TABLE public.youtube_live_session_application_heads (
    video_id text PRIMARY KEY,
    observation_id bigint NOT NULL REFERENCES public.source_observations (id) ON DELETE CASCADE
);

CREATE INDEX idx_youtube_live_session_application_heads_observation
    ON public.youtube_live_session_application_heads (observation_id);

INSERT INTO public.youtube_live_session_application_heads (video_id, observation_id)
SELECT a.entity_key, max(a.observation_id)
FROM public.source_observation_applications a
JOIN public.source_observation_entity_kinds k ON k.id = a.entity_kind_id
WHERE k.name = 'youtube_live_session'
GROUP BY a.entity_key;

REVOKE ALL ON TABLE public.youtube_live_session_application_heads FROM PUBLIC;

DO $acl$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT DISTINCT r.rolname
        FROM pg_catalog.pg_class c
        CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
        JOIN pg_catalog.pg_roles r ON r.oid = a.grantee
        WHERE c.oid = 'public.youtube_live_session_application_heads'::regclass AND a.grantee <> c.relowner
    LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.youtube_live_session_application_heads FROM %I', item.rolname);
    END LOOP;
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT, INSERT, UPDATE ON TABLE public.youtube_live_session_application_heads TO hololive_runtime;
    END IF;
END
$acl$;

CREATE OR REPLACE FUNCTION public.source_observation_retention_protected_ids(requested_ids bigint[]) RETURNS TABLE(id bigint)
    LANGUAGE sql STABLE
    SET search_path TO 'pg_catalog'
    AS $$
    SELECT observation.id FROM public.source_observations AS observation
    WHERE observation.id = ANY(requested_ids) AND (
        EXISTS (SELECT 1 FROM public.source_observation_queue q WHERE q.observation_id = observation.id)
        OR EXISTS (SELECT 1 FROM public.youtube_live_reconciliation_heads h
                   WHERE h.end_candidate_observation_id = observation.id)
        OR EXISTS (SELECT 1 FROM public.youtube_collection_projection_generations g
                   WHERE g.generation = observation.projection_generation AND g.status IN ('CURRENT', 'STAGING'))
        OR EXISTS (SELECT 1 FROM public.youtube_collection_job_leases l
                   JOIN public.source_observation_provenances p ON p.id = observation.provenance_id
                   WHERE l.job_key = p.job_key AND l.slot_state = 'ACTIVE')
        OR EXISTS (SELECT 1 FROM public.youtube_live_pending_ends p
                   JOIN public.youtube_live_sessions s ON s.video_id = p.video_id
                   WHERE p.observation_id = observation.id AND s.status IN ('LIVE', 'UPCOMING'))
    )
    UNION
    SELECT head.observation_id FROM public.youtube_live_session_application_heads head
    JOIN public.youtube_live_sessions s ON s.video_id = head.video_id
    WHERE head.observation_id = ANY(requested_ids) AND s.status IN ('LIVE', 'UPCOMING')
$$;

DROP INDEX public.idx_source_application_entity_latest;

CREATE UNIQUE INDEX uq_source_observation_identity_key
    ON public.source_observations (observation_key, schema_version, contract_generation);
ALTER TABLE public.source_observations DROP CONSTRAINT uq_source_observation_identity;
ALTER TABLE public.source_observations
    ADD CONSTRAINT uq_source_observation_identity UNIQUE USING INDEX uq_source_observation_identity_key;

COMMIT;
