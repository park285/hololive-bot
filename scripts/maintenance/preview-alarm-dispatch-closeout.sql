-- psql -v addressed_delivery_id=2042 -f scripts/maintenance/preview-alarm-dispatch-closeout.sql
\set ON_ERROR_STOP on
\if :{?addressed_delivery_id}
\else
  \echo 'addressed_delivery_id is required'
  \quit 3
\endif
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '10s';
SET LOCAL TIME ZONE 'UTC';
SELECT jsonb_build_object(
    'sendUnitId', send_unit_id::text,
    'targetIds', target_ids,
    'targetRevisions', target_revisions,
    'statusMetadata', status_metadata,
    'originalSha256', original_sha256,
    'memberCount', member_count
)
FROM public.alarm_dispatch_closeout_snapshot(:'addressed_delivery_id'::bigint);
COMMIT;
