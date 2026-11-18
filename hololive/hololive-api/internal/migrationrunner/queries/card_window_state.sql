WITH receipt AS (
    SELECT filename, m.filename IS NOT NULL AND c.filename IS NOT NULL AS paired
    FROM public.schema_migrations m FULL JOIN public.schema_migration_checksums c USING (filename)
), physical AS (
    SELECT (SELECT count(*) FROM pg_catalog.pg_trigger WHERE NOT tgisinternal AND (
               (tgrelid = 'public.source_observations'::regclass AND tgname = 'reject_retired_holodex_channel_profile') OR
               (tgrelid = 'public.source_observation_queue'::regclass AND tgname = 'terminalize_retired_holodex_channel_profile'))) AS triggers,
           (SELECT count(*) FROM pg_catalog.pg_trigger WHERE tgenabled IN ('O','A') AND NOT tgisinternal AND (
               (tgrelid = 'public.source_observations'::regclass AND tgname = 'reject_retired_holodex_channel_profile' AND tgfoid = to_regprocedure('public.reject_retired_holodex_channel_profile()')) OR
               (tgrelid = 'public.source_observation_queue'::regclass AND tgname = 'terminalize_retired_holodex_channel_profile' AND tgfoid = to_regprocedure('public.terminalize_retired_holodex_channel_profile()')))) AS enabled,
           to_regclass('public.member_schedule_card_templates') IS NOT NULL AS card,
           to_regprocedure('public.reject_retired_holodex_channel_profile()') IS NOT NULL AS reject_fn,
           to_regprocedure('public.terminalize_retired_holodex_channel_profile()') IS NOT NULL AS terminal_fn
), ledger AS (
    SELECT array_agg(filename ORDER BY filename COLLATE "C") AS files, bool_and(paired) AS complete FROM receipt
)
SELECT COALESCE(current_database() = 'hololive' AND current_user = 'hololive_migrator' AND inet_server_addr() IS NULL
    AND EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_pgbouncer_auth' AND rolcanlogin AND rolconnlimit = 1)
    AND EXISTS (SELECT FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
                WHERE n.nspname = 'hololive_pooler_auth' AND p.proname = 'lookup' AND p.proargtypes = '25'::oidvector AND p.prosecdef)
    AND complete AND (
        (files = ($1::text[])[1:30] AND triggers = 0 AND NOT (card OR reject_fn OR terminal_fn)
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_stat_activity WHERE datname = current_database() AND usename IN
            ('hololive_runtime','hololive_alarm_worker','hololive_collector_a','hololive_collector_b','hololive_collector_c','hololive_collector_d'))
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_prepared_xacts WHERE database = current_database()))
        OR (files = $1::text[] AND triggers = 2 AND enabled = 2 AND card AND reject_fn AND terminal_fn)
    ), false), COALESCE(files = ($1::text[])[1:30], false) FROM physical, ledger;
