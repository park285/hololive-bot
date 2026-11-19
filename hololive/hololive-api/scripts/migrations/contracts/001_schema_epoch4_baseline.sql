SELECT pg_catalog.to_regclass('public.member_schedule_card_templates') IS NOT NULL
  AND pg_catalog.to_regclass('public.alarms') IS NOT NULL
  AND (SELECT count(*) FROM pg_catalog.pg_trigger WHERE NOT tgisinternal AND tgenabled IN ('O','A') AND (
       (tgrelid = pg_catalog.to_regclass('public.source_observations') AND tgname = 'reject_retired_holodex_channel_profile'
        AND tgfoid = pg_catalog.to_regprocedure('public.reject_retired_holodex_channel_profile()')) OR
       (tgrelid = pg_catalog.to_regclass('public.source_observation_queue') AND tgname = 'terminalize_retired_holodex_channel_profile'
        AND tgfoid = pg_catalog.to_regprocedure('public.terminalize_retired_holodex_channel_profile()')))) = 2
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_alarm_worker' AND NOT (
       pg_catalog.has_schema_privilege(r.oid,'public','USAGE') AND pg_catalog.has_table_privilege(r.oid,'public.alarms','SELECT')
       AND pg_catalog.has_table_privilege(r.oid,'public.alarm_dispatch_events','INSERT')
       AND pg_catalog.has_sequence_privilege(r.oid,'public.alarm_dispatch_events_id_seq','USAGE')
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.source_observations','INSERT')))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_scraper' AND NOT (
       pg_catalog.has_schema_privilege(r.oid,'public','USAGE')
       AND pg_catalog.has_table_privilege(r.oid,'public.source_observations','INSERT')
       AND pg_catalog.has_table_privilege(r.oid,'public.youtube_collection_job_leases','UPDATE')
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.source_observations','DELETE')))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_runtime' AND NOT (
       pg_catalog.has_schema_privilege(r.oid,'public','USAGE')
       AND pg_catalog.has_table_privilege(r.oid,'public.source_observations','SELECT')
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.bot_reply_outbox_replay_audit','UPDATE')));
