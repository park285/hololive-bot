BEGIN;
SET LOCAL lock_timeout = '3s';

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
    SELECT a.observation_id FROM public.source_observation_applications a
    JOIN public.source_observation_entity_kinds k ON k.id = a.entity_kind_id
    JOIN public.youtube_live_sessions s ON s.video_id = a.entity_key
    WHERE a.observation_id = ANY(requested_ids) AND k.name = 'youtube_live_session'
      AND s.status IN ('LIVE', 'UPCOMING')
      AND NOT EXISTS (SELECT 1 FROM public.source_observation_applications newer
                      WHERE newer.entity_kind_id = a.entity_kind_id
                        AND newer.entity_key = a.entity_key
                        AND newer.observation_id > a.observation_id)
$$;

COMMIT;
