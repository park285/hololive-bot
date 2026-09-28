\set ON_ERROR_STOP on
BEGIN READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL TIME ZONE 'UTC';
WITH candidates AS (
    SELECT jsonb_build_object('head', to_jsonb(h), 'canonical_end', p.ended_at) AS snapshot,
           h.video_id
    FROM public.youtube_live_sessions p
    JOIN public.youtube_live_reconciliation_heads h USING (video_id)
    WHERE p.status = 'ENDED' AND h.status = 'LIVE'
      AND p.ended_at IS NOT NULL AND h.last_live_positive_at IS NOT NULL
      AND p.ended_at >= GREATEST(h.last_live_positive_at, h.last_upcoming_positive_at)
      AND h.updated_at < TIMESTAMPTZ '2026-08-17 00:00:00+00'
      AND h.ended_at IS NULL AND h.end_reason IS NULL
      AND h.end_candidate_kind IS NULL AND h.end_candidate_observation_id IS NULL
      AND h.next_end_check_at IS NULL
    ORDER BY h.video_id
    LIMIT 101
), snapshot AS (
    SELECT count(video_id) AS row_count,
           COALESCE(jsonb_agg(snapshot ORDER BY video_id), '[]'::jsonb) AS rows
    FROM candidates
)
SELECT jsonb_build_object('count', row_count, 'digest', md5(rows::text), 'rows', rows)
FROM snapshot;
COMMIT;
