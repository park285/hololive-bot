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
  AND pg_catalog.to_regclass('public.event_grade_collection_targets') IS NULL
  AND pg_catalog.to_regprocedure('public.event_grade_apply_catalog_targets(text,jsonb)') IS NULL
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_attribute a WHERE a.attrelid = pg_catalog.to_regclass('public.event_grade_collection_leases')
       AND a.attname = 'target_generation' AND NOT a.attisdropped)
  AND EXISTS (SELECT FROM pg_catalog.pg_constraint WHERE conname = 'event_grade_collection_leases_chapter_fkey' AND contype = 'f'
       AND conrelid = pg_catalog.to_regclass('public.event_grade_collection_leases')
       AND confrelid = pg_catalog.to_regclass('public.event_grade_chapters') AND convalidated)
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
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_collector_c' AND NOT
       pg_catalog.has_function_privilege(r.oid,'public.event_grade_claim_cutoffs(text,text,integer,integer)','EXECUTE'))
  AND NOT pg_catalog.has_function_privilege('public', 'public.event_grade_claim_cutoffs(text,text,integer,integer)', 'EXECUTE')
  AND pg_catalog.to_regclass('public.youtube_collection_projection_guard') IS NULL
  AND pg_catalog.to_regprocedure('public.refresh_youtube_collection_runtime_bundles_statement()') IS NULL
  AND (SELECT count(*) FROM pg_catalog.pg_trigger t WHERE NOT t.tgisinternal AND t.tgrelid IN (
       pg_catalog.to_regclass('public.youtube_collection_projection_generations'), pg_catalog.to_regclass('public.youtube_collection_targets'))) = 2
  AND (SELECT count(*) FROM pg_catalog.pg_trigger t JOIN pg_catalog.pg_proc p ON p.oid = t.tgfoid AND p.prosecdef
       WHERE NOT t.tgisinternal AND t.tgenabled IN ('O','A') AND t.tgname = 'youtube_collection_runtime_guard' AND t.tgtype = 62
       AND p.oid = pg_catalog.to_regprocedure('public.guard_youtube_collection_runtime_bundles()')
       AND t.tgrelid IN (pg_catalog.to_regclass('public.youtube_collection_projection_generations'), pg_catalog.to_regclass('public.youtube_collection_targets'))) = 2
  AND pg_catalog.to_regclass('public.youtube_collection_runtime_bundles') IS NULL
  AND pg_catalog.to_regprocedure('public.refresh_youtube_collection_runtime_bundles(bigint[])') IS NULL
  AND pg_catalog.to_regprocedure('public.refresh_youtube_collection_runtime_bundle_keys(bigint[],text[],text[])') IS NULL
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_runtime' AND NOT
       pg_catalog.has_function_privilege(r.oid,'public.lock_youtube_collection_runtime_bundles()','EXECUTE'))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_scraper' AND NOT
       pg_catalog.has_function_privilege(r.oid,'public.lock_current_youtube_collection_projection()','EXECUTE'))
  AND pg_catalog.to_regtype('public.youtube_live_review_value') IS NULL
  AND pg_catalog.to_regtype('public.youtube_live_review_session_value') IS NULL
  AND pg_catalog.to_regtype('public.youtube_live_review_head_value') IS NULL
  AND pg_catalog.to_regtype('public.youtube_live_review_pending_value') IS NULL
  AND pg_catalog.to_regprocedure('public.youtube_live_review_receipt_value(jsonb)') IS NULL
  AND pg_catalog.to_regprocedure('public.youtube_live_review_source_facts(text,text,text,text,text,text,boolean,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,smallint,bigint,text,bigint,timestamp with time zone,text,text,text,text,bigint,timestamp with time zone,timestamp with time zone,timestamp with time zone,boolean,boolean,text)') IS NOT NULL
  AND NOT pg_catalog.has_function_privilege('public', 'public.youtube_live_review_source_facts(text,text,text,text,text,text,boolean,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,smallint,bigint,text,bigint,timestamp with time zone,text,text,text,text,bigint,timestamp with time zone,timestamp with time zone,timestamp with time zone,boolean,boolean,text)', 'EXECUTE')
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_attribute a WHERE a.attrelid = pg_catalog.to_regclass('public.youtube_live_review_receipt_facts')
       AND a.attname = 'semantic_value' AND NOT a.attisdropped)
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname='hololive_runtime' AND NOT
       pg_catalog.has_function_privilege(r.oid,'public.youtube_live_review_source_facts(text,text,text,text,text,text,boolean,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,smallint,bigint,text,bigint,timestamp with time zone,text,text,text,text,bigint,timestamp with time zone,timestamp with time zone,timestamp with time zone,boolean,boolean,text)','EXECUTE'))
  AND pg_catalog.to_regprocedure('public.collector_session_instance()') IS NOT NULL
  AND pg_catalog.to_regprocedure('public.youtube_collector_session_instance()') IS NULL
  AND NOT pg_catalog.has_function_privilege('public', 'public.collector_session_instance()', 'EXECUTE')
  AND (SELECT count(*) FROM pg_catalog.pg_proc p WHERE p.pronamespace = 'public'::regnamespace
       AND p.prokind = 'f' AND p.prosrc LIKE '%youtube_collector_session_instance%') = 0
  AND EXISTS (
       SELECT FROM pg_catalog.pg_class c
       JOIN pg_catalog.pg_attribute d ON d.attrelid = c.oid AND d.attname = 'digest' AND d.attnotnull AND d.atttypid = 'bytea'::regtype
       JOIN pg_catalog.pg_attribute e ON e.attrelid = c.oid AND e.attname = 'expires_at' AND e.attnotnull AND e.atttypid = 'timestamptz'::regtype
       WHERE c.oid = pg_catalog.to_regclass('public.bot_webhook_nonces') AND c.relkind = 'r' AND c.relpersistence = 'p'
         AND EXISTS (SELECT FROM pg_catalog.pg_index i WHERE i.indrelid = c.oid AND i.indisprimary AND i.indisvalid AND i.indimmediate
                     AND i.indnkeyatts = 1 AND i.indkey[0] = d.attnum)
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_index i WHERE i.indrelid = c.oid AND i.indisunique AND NOT i.indimmediate
                         AND i.indnkeyatts = 1 AND i.indkey[0] = d.attnum AND i.indexprs IS NULL AND i.indpred IS NULL)
         AND EXISTS (SELECT FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class idx ON idx.oid = i.indexrelid
                     JOIN pg_catalog.pg_am am ON am.oid = idx.relam
                     WHERE i.indrelid = c.oid AND i.indexrelid = pg_catalog.to_regclass('public.bot_webhook_nonces_expiry')
                       AND am.amname = 'btree' AND i.indisvalid AND i.indpred IS NULL AND i.indkey[0] = e.attnum)
         AND NOT EXISTS (SELECT FROM pg_catalog.aclexplode(c.relacl) a WHERE a.grantee <> c.relowner)
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_attribute col CROSS JOIN LATERAL pg_catalog.aclexplode(col.attacl) a
                         WHERE col.attrelid = c.oid AND a.grantee <> c.relowner)
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname IN ('hololive_runtime','hololive_alarm_worker','hololive_scraper',
                         'hololive_collector_a','hololive_collector_b','hololive_collector_c','hololive_collector_d')
                         AND pg_catalog.has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN'))
         AND EXISTS (SELECT FROM pg_catalog.pg_proc p WHERE p.oid = pg_catalog.to_regprocedure('public.reserve_bot_webhook_nonce(bytea,bigint)')
                     AND p.prosecdef AND p.provolatile = 'v' AND p.prorettype = 'boolean'::regtype AND p.proowner = c.relowner
                     AND 'search_path=pg_catalog, pg_temp' = ANY(p.proconfig)
                     AND NOT EXISTS (SELECT FROM pg_catalog.aclexplode(COALESCE(p.proacl,pg_catalog.acldefault('f',p.proowner))) a
                                     WHERE a.grantee <> p.proowner AND (a.grantee IS DISTINCT FROM pg_catalog.to_regrole('hololive_runtime')::oid OR a.is_grantable))))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname = 'hololive_runtime' AND NOT
       pg_catalog.has_function_privilege(r.oid, 'public.reserve_bot_webhook_nonce(bytea,bigint)', 'EXECUTE'))
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname IN ('hololive_alarm_worker','hololive_scraper',
       'hololive_collector_a','hololive_collector_b','hololive_collector_c','hololive_collector_d') AND
       pg_catalog.has_function_privilege(r.oid, 'public.reserve_bot_webhook_nonce(bytea,bigint)', 'EXECUTE'))
  AND EXISTS (
       SELECT FROM pg_catalog.pg_class c
       JOIN pg_catalog.pg_attribute room ON room.attrelid = c.oid AND room.attname = 'room_id' AND room.attnotnull
            AND room.atttypid = 'varchar'::regtype AND room.atttypmod = 104
       JOIN pg_catalog.pg_attribute reason ON reason.attrelid = c.oid AND reason.attname = 'reason' AND reason.attnotnull
            AND reason.atttypid = 'text'::regtype
       JOIN pg_catalog.pg_attribute paused ON paused.attrelid = c.oid AND paused.attname = 'paused_at' AND paused.attnotnull
            AND paused.atttypid = 'timestamptz'::regtype
       WHERE c.oid = pg_catalog.to_regclass('public.kakao_room_delivery_pauses') AND c.relkind = 'r' AND c.relpersistence = 'p'
         AND (SELECT count(*) FROM pg_catalog.pg_attribute a WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped) = 3
         AND EXISTS (SELECT FROM pg_catalog.pg_index i WHERE i.indrelid = c.oid AND i.indisprimary AND i.indisvalid AND i.indimmediate
                     AND i.indnkeyatts = 1 AND i.indkey[0] = room.attnum)
         AND EXISTS (SELECT FROM pg_catalog.pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'c' AND k.convalidated
                     AND pg_catalog.pg_get_constraintdef(k.oid) = 'CHECK ((reason = ''room_not_joined''::text))')
         AND EXISTS (SELECT FROM pg_catalog.pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'c' AND k.convalidated
                     AND pg_catalog.pg_get_constraintdef(k.oid) = 'CHECK ((length((room_id)::text) > 0))')
         AND NOT EXISTS (SELECT FROM pg_catalog.aclexplode(c.relacl) a
                         WHERE a.grantee <> c.relowner AND a.grantee IS DISTINCT FROM pg_catalog.to_regrole('hololive_alarm_worker')::oid
                           AND a.grantee IS DISTINCT FROM pg_catalog.to_regrole('hololive_runtime')::oid)
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_attribute col CROSS JOIN LATERAL pg_catalog.aclexplode(col.attacl) a
                         WHERE col.attrelid = c.oid AND a.grantee <> c.relowner)
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname = 'hololive_alarm_worker' AND NOT (
                         pg_catalog.has_table_privilege(r.oid, c.oid, 'SELECT') AND pg_catalog.has_table_privilege(r.oid, c.oid, 'INSERT')
                         AND NOT pg_catalog.has_table_privilege(r.oid, c.oid, 'UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')))
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname = 'hololive_runtime' AND NOT (
                         pg_catalog.has_table_privilege(r.oid, c.oid, 'SELECT') AND pg_catalog.has_table_privilege(r.oid, c.oid, 'DELETE')
                         AND NOT pg_catalog.has_table_privilege(r.oid, c.oid, 'INSERT,UPDATE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')))
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname IN ('hololive_scraper',
                         'hololive_collector_a','hololive_collector_b','hololive_collector_c','hololive_collector_d')
                         AND pg_catalog.has_table_privilege(r.oid, c.oid, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')))
  AND EXISTS (SELECT FROM pg_catalog.pg_proc p
       WHERE p.oid = pg_catalog.to_regprocedure('public.source_observation_retention_protected_ids(bigint[])')
         AND p.provolatile = 's' AND NOT p.prosecdef AND p.proconfig = ARRAY['search_path=pg_catalog']
         AND pg_catalog.strpos(p.prosrc, 'WHERE head.observation_id = ANY(requested_ids)') > 0
         AND pg_catalog.strpos(p.prosrc, 'source_observation_applications') = 0)
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname = 'hololive_runtime'
       AND NOT pg_catalog.has_function_privilege(r.oid, 'public.source_observation_retention_protected_ids(bigint[])', 'EXECUTE'))
  AND EXISTS (SELECT FROM pg_catalog.pg_class c
       WHERE c.oid = pg_catalog.to_regclass('public.schedule_poster_notices') AND c.relkind = 'r' AND c.relpersistence = 'p'
         AND (SELECT count(*) FROM pg_catalog.pg_attribute a WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped) = 10
         AND EXISTS (SELECT FROM pg_catalog.pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'f' AND k.convalidated
                     AND k.confrelid = pg_catalog.to_regclass('public.schedule_posters') AND k.confdeltype = 'c')
         AND EXISTS (SELECT FROM pg_catalog.pg_index i WHERE i.indexrelid = pg_catalog.to_regclass('public.schedule_poster_notices_published_key')
                     AND i.indrelid = c.oid AND i.indisunique AND i.indisvalid AND i.indpred IS NOT NULL)
         AND EXISTS (SELECT FROM pg_catalog.pg_index i WHERE i.indexrelid = pg_catalog.to_regclass('public.schedule_poster_notices_changed_key')
                     AND i.indrelid = c.oid AND i.indisunique AND i.indisvalid AND i.indpred IS NOT NULL)
         AND NOT EXISTS (SELECT FROM pg_catalog.aclexplode(c.relacl) a
                         WHERE a.grantee <> c.relowner AND a.grantee IS DISTINCT FROM pg_catalog.to_regrole('hololive_runtime')::oid)
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname = 'hololive_runtime' AND NOT (
                         pg_catalog.has_table_privilege(r.oid, c.oid, 'SELECT') AND pg_catalog.has_table_privilege(r.oid, c.oid, 'INSERT')
                         AND pg_catalog.has_table_privilege(r.oid, c.oid, 'UPDATE')
                         AND NOT pg_catalog.has_table_privilege(r.oid, c.oid, 'DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')))
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname IN ('hololive_alarm_worker', 'hololive_scraper',
                         'hololive_collector_a','hololive_collector_b','hololive_collector_c','hololive_collector_d')
                         AND pg_catalog.has_table_privilege(r.oid, c.oid, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')))
  AND pg_catalog.to_regclass('public.idx_source_application_entity_latest') IS NULL
  AND EXISTS (SELECT FROM pg_catalog.pg_constraint k
       WHERE k.conrelid = pg_catalog.to_regclass('public.source_observations') AND k.conname = 'uq_source_observation_identity'
         AND k.contype = 'u' AND k.convalidated
         AND pg_catalog.pg_get_constraintdef(k.oid) = 'UNIQUE (observation_key, schema_version, contract_generation)')
  AND EXISTS (SELECT FROM pg_catalog.pg_class c
       WHERE c.oid = pg_catalog.to_regclass('public.youtube_live_session_application_heads') AND c.relkind = 'r' AND c.relpersistence = 'p'
         AND (SELECT count(*) FROM pg_catalog.pg_attribute a WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped) = 2
         AND EXISTS (SELECT FROM pg_catalog.pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'p' AND k.convalidated
                     AND pg_catalog.pg_get_constraintdef(k.oid) = 'PRIMARY KEY (video_id)')
         AND EXISTS (SELECT FROM pg_catalog.pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'f' AND k.convalidated
                     AND k.confrelid = pg_catalog.to_regclass('public.source_observations') AND k.confdeltype = 'c')
         AND EXISTS (SELECT FROM pg_catalog.pg_index i
                     WHERE i.indexrelid = pg_catalog.to_regclass('public.idx_youtube_live_session_application_heads_observation')
                       AND i.indrelid = c.oid AND i.indisvalid)
         AND NOT EXISTS (SELECT FROM pg_catalog.aclexplode(c.relacl) a
                         WHERE a.grantee <> c.relowner AND a.grantee IS DISTINCT FROM pg_catalog.to_regrole('hololive_runtime')::oid)
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname = 'hololive_runtime' AND NOT (
                         pg_catalog.has_table_privilege(r.oid, c.oid, 'SELECT') AND pg_catalog.has_table_privilege(r.oid, c.oid, 'INSERT')
                         AND pg_catalog.has_table_privilege(r.oid, c.oid, 'UPDATE')
                         AND NOT pg_catalog.has_table_privilege(r.oid, c.oid, 'DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')))
         AND NOT EXISTS (SELECT FROM pg_catalog.pg_roles r WHERE r.rolname IN ('hololive_alarm_worker', 'hololive_scraper',
                         'hololive_collector_a','hololive_collector_b','hololive_collector_c','hololive_collector_d')
                         AND pg_catalog.has_table_privilege(r.oid, c.oid, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')))
  AND EXISTS (SELECT FROM pg_catalog.pg_attribute a WHERE a.attrelid = pg_catalog.to_regclass('public.source_observations')
       AND a.attname = 'schedule_observed_at' AND NOT a.attisdropped AND NOT a.attnotnull
       AND a.atttypid = pg_catalog.to_regtype('jsonb'))
  AND EXISTS (SELECT FROM pg_catalog.pg_constraint k WHERE k.conrelid = pg_catalog.to_regclass('public.source_observations')
       AND k.conname = 'chk_source_observation_schedule_observed_at' AND k.contype = 'c' AND k.convalidated
       AND pg_catalog.strpos(pg_catalog.pg_get_constraintdef(k.oid), '<= 4096') > 0)
  AND NOT EXISTS (SELECT FROM pg_catalog.pg_attribute a WHERE a.attrelid = pg_catalog.to_regclass('public.source_observations')
       AND a.attname = 'created_at' AND NOT a.attisdropped);
