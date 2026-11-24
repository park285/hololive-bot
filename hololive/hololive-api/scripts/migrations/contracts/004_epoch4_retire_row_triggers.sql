SELECT pg_catalog.to_regclass('public.member_schedule_card_templates') IS NOT NULL
  AND pg_catalog.to_regclass('public.alarms') IS NOT NULL
  AND pg_catalog.to_regprocedure('public.reject_retired_holodex_channel_profile()') IS NULL
  AND pg_catalog.to_regprocedure('public.terminalize_retired_holodex_channel_profile()') IS NULL
  AND pg_catalog.to_regprocedure('public.scrub_bot_command_execution_terminal_summary()') IS NULL
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_trigger WHERE NOT tgisinternal AND tgname IN (
       'reject_retired_holodex_channel_profile', 'terminalize_retired_holodex_channel_profile', 'bot_command_execution_terminal_summary_scrub'))
  AND EXISTS (SELECT FROM pg_catalog.pg_constraint WHERE conrelid = pg_catalog.to_regclass('public.bot_command_executions')
       AND conname = 'chk_bot_command_executions_terminal_summary_scrubbed' AND contype = 'c' AND convalidated)
  AND (SELECT count(*) FROM pg_catalog.pg_trigger t WHERE NOT t.tgisinternal AND t.tgenabled IN ('O','A')
       AND t.tgrelid = pg_catalog.to_regclass('public.bot_reply_outbox') AND t.tgname = 'bot_reply_outbox_replay_claim_audit'
       AND t.tgfoid = pg_catalog.to_regprocedure('public.append_bot_reply_outbox_replay_claim_audit()')
       AND pg_catalog.pg_get_triggerdef(t.oid) = 'CREATE TRIGGER bot_reply_outbox_replay_claim_audit BEFORE UPDATE ON public.bot_reply_outbox FOR EACH ROW WHEN (((new.status = ''submitting''::text) AND (old.status <> ''submitting''::text) AND (new.operator_replay_grants > 0))) EXECUTE FUNCTION append_bot_reply_outbox_replay_claim_audit()') = 1
  AND (SELECT c.reloptions FROM pg_catalog.pg_class c WHERE c.oid = pg_catalog.to_regclass('public.source_collection_checkpoints')) = ARRAY['fillfactor=80']
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
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.bot_reply_outbox_replay_audit','UPDATE')))
  AND (SELECT count(*) FROM pg_catalog.pg_enum e WHERE e.enumtypid = pg_catalog.to_regtype('public.alarm_type') AND e.enumlabel = 'EVENT_GRADE') = 1
  AND pg_catalog.to_regclass('public.event_grade_catalogs') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_chapters') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_borders') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_requests') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_regions') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_collection_targets') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_collection_leases') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_observations') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_consumer_offsets') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_requests_due') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_requests_pending') IS NOT NULL
  AND pg_catalog.to_regclass('public.event_grade_chapters_due') IS NOT NULL
  AND (SELECT count(*) FROM pg_catalog.pg_proc p WHERE p.oid IN (
       pg_catalog.to_regprocedure('public.event_grade_claim_catalog(text,text,integer)'),
       pg_catalog.to_regprocedure('public.event_grade_claim_cutoffs(text,text,integer,integer)'),
       pg_catalog.to_regprocedure('public.event_grade_release(text,text,bigint)'),
       pg_catalog.to_regprocedure('public.event_grade_publish(text,text,bigint,timestamp with time zone,timestamp with time zone,jsonb)')) AND p.prosecdef) = 4
  AND NOT pg_catalog.has_function_privilege('public', 'public.event_grade_publish(text,text,bigint,timestamp with time zone,timestamp with time zone,jsonb)', 'EXECUTE')
  AND (SELECT count(*) FROM public.event_grade_regions) = 3
  AND (SELECT count(*) FROM public.event_grade_consumer_offsets) = 3
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_alarm_worker' AND NOT (
       pg_catalog.has_table_privilege(r.oid,'public.event_grade_requests','INSERT')
       AND pg_catalog.has_table_privilege(r.oid,'public.event_grade_observations','SELECT')
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.event_grade_observations','INSERT')
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.event_grade_observations','UPDATE')
       AND pg_catalog.has_table_privilege(r.oid,'public.event_grade_consumer_offsets','UPDATE')
       AND pg_catalog.has_table_privilege(r.oid,'public.event_grade_collection_leases','SELECT')
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.event_grade_collection_leases','UPDATE')
       AND NOT pg_catalog.has_function_privilege(r.oid,'public.event_grade_publish(text,text,bigint,timestamp with time zone,timestamp with time zone,jsonb)','EXECUTE')))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_collector_c' AND NOT (
       pg_catalog.has_function_privilege(r.oid,'public.event_grade_publish(text,text,bigint,timestamp with time zone,timestamp with time zone,jsonb)','EXECUTE')
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.event_grade_observations','INSERT')
       AND NOT pg_catalog.has_table_privilege(r.oid,'public.event_grade_requests','SELECT')))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname IN ('hololive_scraper','hololive_runtime','hololive_collector_a','hololive_collector_b','hololive_collector_d') AND (
       pg_catalog.has_function_privilege(r.oid,'public.event_grade_publish(text,text,bigint,timestamp with time zone,timestamp with time zone,jsonb)','EXECUTE')
       OR pg_catalog.has_table_privilege(r.oid,'public.event_grade_observations','INSERT')))
  AND pg_catalog.to_regprocedure('public.collector_session_instance()') IS NOT NULL
  AND pg_catalog.to_regprocedure('public.youtube_collector_session_instance()') IS NULL
  AND NOT pg_catalog.has_function_privilege('public', 'public.collector_session_instance()', 'EXECUTE')
  AND (SELECT count(*) FROM pg_catalog.pg_proc p WHERE p.pronamespace = 'public'::regnamespace
       AND p.prokind = 'f' AND p.prosrc LIKE '%youtube_collector_session_instance%') = 0;
