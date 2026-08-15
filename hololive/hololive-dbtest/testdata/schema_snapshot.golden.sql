-- hololive schema snapshot (deterministic pg_catalog serialization)
-- objects: enum types, tables, columns, constraints, indexes, reloptions, triggers, sequences, functions
-- regenerate: SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest

ENUM alarm_type
  LIVE
  COMMUNITY
  SHORTS
  BIRTHDAY
  ANNIVERSARY

TABLE acl_rooms
  COLUMN id integer NOT NULL DEFAULT nextval('acl_rooms_id_seq'::regclass)
  COLUMN room_id character varying(100) NOT NULL
  COLUMN list_type character varying(16) NOT NULL DEFAULT 'whitelist'::character varying
  CONSTRAINT chk_acl_rooms_list_type_vocab CHECK (((list_type)::text = ANY ((ARRAY['whitelist'::character varying, 'blacklist'::character varying])::text[])))
  CONSTRAINT acl_rooms_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX idx_room_list ON public.acl_rooms USING btree (room_id, list_type)

TABLE acl_settings
  COLUMN id integer NOT NULL DEFAULT nextval('acl_settings_id_seq'::regclass)
  COLUMN key character varying(64) NOT NULL
  COLUMN value text
  CONSTRAINT acl_settings_pkey PRIMARY KEY (id)
  CONSTRAINT acl_settings_key_key UNIQUE (key)

TABLE alarm_dispatch_admin_actions
  COLUMN id bigint NOT NULL DEFAULT nextval('alarm_dispatch_admin_actions_id_seq'::regclass)
  COLUMN delivery_id bigint
  COLUMN action text NOT NULL
  COLUMN operator_id text NOT NULL
  COLUMN reason text NOT NULL
  COLUMN from_status text NOT NULL DEFAULT ''::text
  COLUMN to_status text NOT NULL DEFAULT ''::text
  COLUMN duplicate_risk_ack boolean NOT NULL DEFAULT false
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT alarm_dispatch_admin_actions_action_check CHECK (((length(action) > 0) AND (length(action) <= 128)))
  CONSTRAINT alarm_dispatch_admin_actions_operator_check CHECK (((length(operator_id) > 0) AND (length(operator_id) <= 128)))
  CONSTRAINT alarm_dispatch_admin_actions_reason_check CHECK (((length(reason) > 0) AND (length(reason) <= 1024)))
  CONSTRAINT alarm_dispatch_admin_actions_delivery_id_fkey FOREIGN KEY (delivery_id) REFERENCES alarm_dispatch_deliveries(id) ON DELETE SET NULL
  CONSTRAINT alarm_dispatch_admin_actions_pkey PRIMARY KEY (id)
  INDEX CREATE INDEX idx_alarm_dispatch_admin_actions_delivery ON public.alarm_dispatch_admin_actions USING btree (delivery_id)

TABLE alarm_dispatch_deliveries
  OPTIONS autovacuum_analyze_scale_factor=0.02,autovacuum_analyze_threshold=50,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=50
  COLUMN id bigint NOT NULL DEFAULT nextval('alarm_dispatch_deliveries_id_seq'::regclass)
  COLUMN event_id bigint NOT NULL
  COLUMN room_id character varying(100) NOT NULL
  COLUMN dedupe_key text NOT NULL
  COLUMN claim_keys text[] NOT NULL DEFAULT ARRAY[]::text[]
  COLUMN delivery_context jsonb NOT NULL DEFAULT '{}'::jsonb
  COLUMN status text NOT NULL DEFAULT 'pending'::text
  COLUMN attempt_count integer NOT NULL DEFAULT 0
  COLUMN next_attempt_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN locked_by text
  COLUMN locked_at timestamp with time zone
  COLUMN lock_expires_at timestamp with time zone
  COLUMN sending_started_at timestamp with time zone
  COLUMN sent_at timestamp with time zone
  COLUMN dlq_at timestamp with time zone
  COLUMN quarantined_at timestamp with time zone
  COLUMN cancelled_at timestamp with time zone
  COLUMN last_error_code text NOT NULL DEFAULT ''::text
  COLUMN last_error text NOT NULL DEFAULT ''::text
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN dispatch_group_key text
  COLUMN send_unit_id bigint
  CONSTRAINT alarm_dispatch_deliveries_attempt_check CHECK ((attempt_count >= 0))
  CONSTRAINT alarm_dispatch_deliveries_dedupe_key_check CHECK (((length(dedupe_key) > 0) AND (length(dedupe_key) <= 768)))
  CONSTRAINT alarm_dispatch_deliveries_dispatch_group_key_check CHECK (((dispatch_group_key IS NULL) OR ((length(dispatch_group_key) > 0) AND (length(dispatch_group_key) <= 768))))
  CONSTRAINT alarm_dispatch_deliveries_room_id_check CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100)))
  CONSTRAINT alarm_dispatch_deliveries_send_unit_pair_check CHECK (((dispatch_group_key IS NULL) = (send_unit_id IS NULL)))
  CONSTRAINT alarm_dispatch_deliveries_status_check CHECK ((status = ANY (ARRAY['shadowed'::text, 'pending'::text, 'retry'::text, 'leased'::text, 'sending'::text, 'sent'::text, 'dlq'::text, 'quarantined'::text, 'cancelled'::text])))
  CONSTRAINT chk_alarm_dispatch_deliveries_last_error_size CHECK ((octet_length(last_error) <= 8192))
  CONSTRAINT chk_alarm_dispatch_deliveries_state_shape CHECK ((((status <> 'leased'::text) OR ((locked_by IS NOT NULL) AND (locked_at IS NOT NULL) AND (lock_expires_at IS NOT NULL))) AND ((status <> 'sending'::text) OR ((locked_by IS NOT NULL) AND (locked_at IS NOT NULL) AND (lock_expires_at IS NOT NULL) AND (sending_started_at IS NOT NULL))) AND ((status <> 'sent'::text) OR (sent_at IS NOT NULL)) AND ((status <> 'dlq'::text) OR (dlq_at IS NOT NULL)) AND ((status <> 'quarantined'::text) OR (quarantined_at IS NOT NULL)) AND ((status <> 'cancelled'::text) OR (cancelled_at IS NOT NULL))))
  CONSTRAINT alarm_dispatch_deliveries_event_id_fkey FOREIGN KEY (event_id) REFERENCES alarm_dispatch_events(id) ON DELETE RESTRICT
  CONSTRAINT alarm_dispatch_deliveries_send_unit_fk FOREIGN KEY (send_unit_id) REFERENCES alarm_dispatch_send_units(id) ON DELETE RESTRICT
  CONSTRAINT alarm_dispatch_deliveries_pkey PRIMARY KEY (id)
  CONSTRAINT alarm_dispatch_deliveries_dedupe_key_key UNIQUE (dedupe_key)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_cancelled_retention ON public.alarm_dispatch_deliveries USING btree (cancelled_at, id) WHERE (status = 'cancelled'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_dlq_retention ON public.alarm_dispatch_deliveries USING btree (dlq_at, id) WHERE (status = 'dlq'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_due ON public.alarm_dispatch_deliveries USING btree (next_attempt_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retry'::text]))
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_event_id ON public.alarm_dispatch_deliveries USING btree (event_id)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_leased_expired ON public.alarm_dispatch_deliveries USING btree (lock_expires_at, id) WHERE (status = 'leased'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_quarantined_retention ON public.alarm_dispatch_deliveries USING btree (quarantined_at, id) WHERE (status = 'quarantined'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_room_created ON public.alarm_dispatch_deliveries USING btree (room_id, created_at DESC)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_send_unit ON public.alarm_dispatch_deliveries USING btree (send_unit_id) WHERE (send_unit_id IS NOT NULL)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_send_unit_due ON public.alarm_dispatch_deliveries USING btree (send_unit_id, next_attempt_at, id) WHERE ((send_unit_id IS NOT NULL) AND (status = ANY (ARRAY['pending'::text, 'retry'::text])))
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_sending_stale ON public.alarm_dispatch_deliveries USING btree (sending_started_at, id) WHERE (status = 'sending'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_sent_event_room ON public.alarm_dispatch_deliveries USING btree (event_id, room_id, sent_at DESC) WHERE ((status = 'sent'::text) AND (sent_at IS NOT NULL))
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_sent_retention ON public.alarm_dispatch_deliveries USING btree (sent_at, id) WHERE (status = 'sent'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_status_created ON public.alarm_dispatch_deliveries USING btree (status, created_at DESC)

TABLE alarm_dispatch_event_collisions
  COLUMN id bigint NOT NULL DEFAULT nextval('alarm_dispatch_event_collisions_id_seq'::regclass)
  COLUMN existing_event_id bigint
  COLUMN event_key text NOT NULL
  COLUMN existing_payload_hash character(64) NOT NULL
  COLUMN incoming_payload_hash character(64) NOT NULL
  COLUMN alarm_type alarm_type NOT NULL
  COLUMN channel_id character varying(64) NOT NULL DEFAULT ''::character varying
  COLUMN stream_id character varying(64) NOT NULL DEFAULT ''::character varying
  COLUMN category text NOT NULL DEFAULT ''::text
  COLUMN payload_schema_version smallint NOT NULL DEFAULT 1
  COLUMN payload jsonb NOT NULL
  COLUMN status text NOT NULL DEFAULT 'detected'::text
  COLUMN last_error text NOT NULL DEFAULT 'event_key payload_hash conflict'::text
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT alarm_dispatch_event_collisions_event_key_check CHECK (((length(event_key) > 0) AND (length(event_key) <= 512)))
  CONSTRAINT alarm_dispatch_event_collisions_existing_payload_hash_check CHECK ((existing_payload_hash ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT alarm_dispatch_event_collisions_incoming_payload_hash_check CHECK ((incoming_payload_hash ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT alarm_dispatch_event_collisions_payload_room_agnostic_check CHECK (((NOT (payload ? 'room_id'::text)) AND (NOT (payload ? 'roomId'::text)) AND (NOT (payload ? 'room'::text)) AND (NOT (payload ? 'users'::text)) AND (NOT ((payload -> 'notification'::text) ? 'room_id'::text)) AND (NOT ((payload -> 'notification'::text) ? 'roomId'::text)) AND (NOT ((payload -> 'notification'::text) ? 'room'::text)) AND (NOT ((payload -> 'notification'::text) ? 'users'::text))))
  CONSTRAINT alarm_dispatch_event_collisions_status_check CHECK ((status = ANY (ARRAY['detected'::text, 'acknowledged'::text, 'resolved'::text])))
  CONSTRAINT alarm_dispatch_event_collisions_existing_event_id_fkey FOREIGN KEY (existing_event_id) REFERENCES alarm_dispatch_events(id) ON DELETE SET NULL
  CONSTRAINT alarm_dispatch_event_collisions_pkey PRIMARY KEY (id)
  CONSTRAINT alarm_dispatch_event_collisio_event_key_incoming_payload_ha_key UNIQUE (event_key, incoming_payload_hash)
  INDEX CREATE INDEX idx_alarm_dispatch_event_collisions_existing_event ON public.alarm_dispatch_event_collisions USING btree (existing_event_id) WHERE (existing_event_id IS NOT NULL)

TABLE alarm_dispatch_events
  COLUMN id bigint NOT NULL DEFAULT nextval('alarm_dispatch_events_id_seq'::regclass)
  COLUMN event_key text NOT NULL
  COLUMN payload_hash character(64) NOT NULL
  COLUMN alarm_type alarm_type NOT NULL
  COLUMN channel_id character varying(64) NOT NULL DEFAULT ''::character varying
  COLUMN stream_id character varying(64) NOT NULL DEFAULT ''::character varying
  COLUMN category text NOT NULL DEFAULT ''::text
  COLUMN payload_schema_version smallint NOT NULL DEFAULT 1
  COLUMN payload jsonb NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT alarm_dispatch_events_event_key_check CHECK (((length(event_key) > 0) AND (length(event_key) <= 512)))
  CONSTRAINT alarm_dispatch_events_payload_hash_check CHECK ((payload_hash ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT alarm_dispatch_events_payload_notification_room_agnostic_check CHECK (((NOT (payload ? 'room_id'::text)) AND (NOT (payload ? 'roomId'::text)) AND (NOT (payload ? 'room'::text)) AND (NOT (payload ? 'users'::text)) AND (NOT ((payload -> 'notification'::text) ? 'room_id'::text)) AND (NOT ((payload -> 'notification'::text) ? 'roomId'::text)) AND (NOT ((payload -> 'notification'::text) ? 'room'::text)) AND (NOT ((payload -> 'notification'::text) ? 'users'::text))))
  CONSTRAINT alarm_dispatch_events_pkey PRIMARY KEY (id)
  CONSTRAINT alarm_dispatch_events_event_key_key UNIQUE (event_key)
  INDEX CREATE INDEX idx_alarm_dispatch_events_created ON public.alarm_dispatch_events USING btree (created_at, id)
  INDEX CREATE INDEX idx_alarm_dispatch_events_live_stream_created ON public.alarm_dispatch_events USING btree (stream_id, created_at DESC) WHERE (alarm_type = 'LIVE'::alarm_type)

TABLE alarm_dispatch_send_units
  COLUMN id bigint NOT NULL DEFAULT nextval('alarm_dispatch_send_units_id_seq'::regclass)
  COLUMN unit_key character(64) NOT NULL
  COLUMN dispatch_group_key text NOT NULL
  COLUMN room_id character varying(100) NOT NULL
  COLUMN client_request_id text NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT alarm_dispatch_send_units_client_request_id_check CHECK ((client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'::text))
  CONSTRAINT alarm_dispatch_send_units_group_key_check CHECK (((length(dispatch_group_key) > 0) AND (length(dispatch_group_key) <= 768)))
  CONSTRAINT alarm_dispatch_send_units_room_id_check CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100)))
  CONSTRAINT alarm_dispatch_send_units_unit_key_check CHECK ((unit_key ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT alarm_dispatch_send_units_pkey PRIMARY KEY (id)
  CONSTRAINT alarm_dispatch_send_units_client_request_id_key UNIQUE (client_request_id)
  CONSTRAINT alarm_dispatch_send_units_unit_key_key UNIQUE (unit_key)

TABLE alarms
  COLUMN id integer NOT NULL DEFAULT nextval('alarms_id_seq'::regclass)
  COLUMN room_id character varying(100) NOT NULL
  COLUMN user_id character varying(64) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN member_name text
  COLUMN room_name character varying(255)
  COLUMN user_name character varying(200)
  COLUMN created_at timestamp with time zone DEFAULT now()
  COLUMN alarm_types alarm_type[] NOT NULL DEFAULT ARRAY['LIVE'::alarm_type]
  CONSTRAINT alarms_pkey PRIMARY KEY (id)
  CONSTRAINT alarms_room_channel_unique UNIQUE (room_id, channel_id)
  INDEX CREATE INDEX idx_alarms_alarm_types_gin ON public.alarms USING gin (alarm_types)
  INDEX CREATE INDEX idx_alarms_channel_created ON public.alarms USING btree (channel_id, created_at)
  INDEX CREATE INDEX idx_alarms_channel_member_latest ON public.alarms USING btree (channel_id, created_at DESC) WHERE ((member_name IS NOT NULL) AND (member_name <> ''::text))
  INDEX CREATE INDEX idx_alarms_room_created ON public.alarms USING btree (room_id, created_at)

TABLE auth_password_reset_tokens
  COLUMN token_hash text NOT NULL
  COLUMN user_id text NOT NULL
  COLUMN expires_at timestamp with time zone NOT NULL
  COLUMN used_at timestamp with time zone
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
  CONSTRAINT auth_password_reset_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE CASCADE
  CONSTRAINT auth_password_reset_tokens_pkey PRIMARY KEY (token_hash)
  INDEX CREATE INDEX idx_auth_reset_tokens_user_unused ON public.auth_password_reset_tokens USING btree (user_id) WHERE (used_at IS NULL)

TABLE auth_users
  COLUMN id text NOT NULL
  COLUMN email text NOT NULL
  COLUMN password_hash text NOT NULL
  COLUMN display_name text NOT NULL
  COLUMN avatar_url text
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
  CONSTRAINT auth_users_pkey PRIMARY KEY (id)
  CONSTRAINT auth_users_email_key UNIQUE (email)

TABLE bot_command_executions
  COLUMN id bigint NOT NULL DEFAULT nextval('bot_command_executions_id_seq'::regclass)
  COLUMN message_id text NOT NULL
  COLUMN command_kind text NOT NULL DEFAULT ''::text
  COLUMN status text NOT NULL DEFAULT 'claimed'::text
  COLUMN claim_token text NOT NULL
  COLUMN result_summary text NOT NULL DEFAULT ''::text
  COLUMN claimed_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN completed_at timestamp with time zone
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_bot_command_executions_claim_token_size CHECK (((length(claim_token) > 0) AND (length(claim_token) <= 256)))
  CONSTRAINT chk_bot_command_executions_command_kind_size CHECK ((length(command_kind) <= 128))
  CONSTRAINT chk_bot_command_executions_message_id_size CHECK (((length(message_id) > 0) AND (length(message_id) <= 512)))
  CONSTRAINT chk_bot_command_executions_result_summary_size CHECK ((octet_length(result_summary) <= 2048))
  CONSTRAINT chk_bot_command_executions_state_shape CHECK (((status = 'claimed'::text) OR (completed_at IS NOT NULL)))
  CONSTRAINT chk_bot_command_executions_status_vocab CHECK ((status = ANY (ARRAY['claimed'::text, 'succeeded'::text, 'failed'::text, 'outcome_unknown'::text])))
  CONSTRAINT chk_bot_command_executions_terminal_summary_scrubbed CHECK (((status <> ALL (ARRAY['succeeded'::text, 'failed'::text, 'outcome_unknown'::text])) OR (result_summary = status)))
  CONSTRAINT bot_command_executions_pkey PRIMARY KEY (id)
  CONSTRAINT bot_command_executions_message_id_key UNIQUE (message_id)
  INDEX CREATE INDEX idx_bot_command_executions_status_claimed ON public.bot_command_executions USING btree (claimed_at, id) WHERE (status = 'claimed'::text)
  INDEX CREATE INDEX idx_bot_command_executions_terminal_updated ON public.bot_command_executions USING btree (updated_at, id) WHERE (status = ANY (ARRAY['succeeded'::text, 'failed'::text, 'outcome_unknown'::text]))
  TRIGGER CREATE TRIGGER bot_command_execution_terminal_summary_scrub BEFORE INSERT OR UPDATE OF status, result_summary ON bot_command_executions FOR EACH ROW WHEN (new.status = ANY (ARRAY['succeeded'::text, 'failed'::text, 'outcome_unknown'::text])) EXECUTE FUNCTION scrub_bot_command_execution_terminal_summary()

TABLE bot_reply_outbox
  COLUMN id bigint NOT NULL DEFAULT nextval('bot_reply_outbox_id_seq'::regclass)
  COLUMN message_id text NOT NULL
  COLUMN phase text NOT NULL
  COLUMN ordinal bigint NOT NULL
  COLUMN room_id text NOT NULL
  COLUMN payload jsonb
  COLUMN payload_hash character(64) NOT NULL
  COLUMN client_request_id text NOT NULL
  COLUMN status text NOT NULL DEFAULT 'pending'::text
  COLUMN attempts integer NOT NULL DEFAULT 0
  COLUMN first_attempt_at timestamp with time zone
  COLUMN iris_request_id text NOT NULL DEFAULT ''::text
  COLUMN claim_token text
  COLUMN lease_until timestamp with time zone
  COLUMN last_error text NOT NULL DEFAULT ''::text
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN available_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN operator_replay_grants integer NOT NULL DEFAULT 0
  CONSTRAINT chk_bot_reply_outbox_attempts CHECK ((attempts >= 0))
  CONSTRAINT chk_bot_reply_outbox_client_request_id CHECK ((client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'::text))
  CONSTRAINT chk_bot_reply_outbox_iris_request_id_size CHECK ((length(iris_request_id) <= 256))
  CONSTRAINT chk_bot_reply_outbox_last_error_size CHECK ((octet_length(last_error) <= 8192))
  CONSTRAINT chk_bot_reply_outbox_message_id_size CHECK (((length(message_id) > 0) AND (length(message_id) <= 512)))
  CONSTRAINT chk_bot_reply_outbox_operator_replay_grants CHECK ((operator_replay_grants >= 0))
  CONSTRAINT chk_bot_reply_outbox_ordinal CHECK ((ordinal >= 0))
  CONSTRAINT chk_bot_reply_outbox_payload_hash CHECK ((payload_hash ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT chk_bot_reply_outbox_phase_size CHECK (((length(phase) > 0) AND (length(phase) <= 32)))
  CONSTRAINT chk_bot_reply_outbox_room_id_size CHECK (((length(room_id) > 0) AND (length(room_id) <= 256)))
  CONSTRAINT chk_bot_reply_outbox_state_shape CHECK ((((status <> ALL (ARRAY['submitting'::text, 'accepted'::text])) OR ((claim_token IS NOT NULL) AND (lease_until IS NOT NULL) AND (first_attempt_at IS NOT NULL))) AND ((status <> 'accepted'::text) OR (length(iris_request_id) > 0)) AND ((status = ANY (ARRAY['handoff_completed'::text, 'dead'::text, 'permanent_conflict'::text])) OR (payload IS NOT NULL))))
  CONSTRAINT chk_bot_reply_outbox_status_vocab CHECK ((status = ANY (ARRAY['pending'::text, 'submitting'::text, 'accepted'::text, 'handoff_completed'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text, 'dead'::text, 'permanent_conflict'::text, 'manual_review'::text])))
  CONSTRAINT bot_reply_outbox_pkey PRIMARY KEY (id)
  CONSTRAINT bot_reply_outbox_client_request_id_key UNIQUE (client_request_id)
  CONSTRAINT bot_reply_outbox_message_id_phase_ordinal_key UNIQUE (message_id, phase, ordinal)
  INDEX CREATE INDEX idx_bot_reply_outbox_due_available ON public.bot_reply_outbox USING btree (available_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text]))
  INDEX CREATE INDEX idx_bot_reply_outbox_lease_expiry ON public.bot_reply_outbox USING btree (lease_until, id) WHERE (status = ANY (ARRAY['submitting'::text, 'accepted'::text]))
  INDEX CREATE INDEX idx_bot_reply_outbox_manual_review_updated ON public.bot_reply_outbox USING btree (updated_at, id) WHERE (status = 'manual_review'::text)
  INDEX CREATE INDEX idx_bot_reply_outbox_message ON public.bot_reply_outbox USING btree (message_id, ordinal)
  INDEX CREATE INDEX idx_bot_reply_outbox_room_active ON public.bot_reply_outbox USING btree (room_id, id) WHERE (status = ANY (ARRAY['pending'::text, 'submitting'::text, 'accepted'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text]))
  INDEX CREATE INDEX idx_bot_reply_outbox_terminal_updated ON public.bot_reply_outbox USING btree (updated_at, id) WHERE (status = ANY (ARRAY['handoff_completed'::text, 'dead'::text, 'permanent_conflict'::text]))
  TRIGGER CREATE TRIGGER bot_reply_outbox_replay_claim_audit BEFORE UPDATE ON bot_reply_outbox FOR EACH ROW EXECUTE FUNCTION append_bot_reply_outbox_replay_claim_audit()

TABLE bot_reply_outbox_replay_audit
  COLUMN id bigint NOT NULL DEFAULT nextval('bot_reply_outbox_replay_audit_id_seq'::regclass)
  COLUMN outbox_id bigint NOT NULL
  COLUMN grant_number integer NOT NULL
  COLUMN event_type text NOT NULL
  COLUMN actor text NOT NULL
  COLUMN reason text NOT NULL
  COLUMN recorded_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_bot_reply_outbox_replay_audit_actor CHECK ((actor ~ '^[A-Za-z0-9._:@-]{1,64}$'::text))
  CONSTRAINT chk_bot_reply_outbox_replay_audit_event_type CHECK ((event_type = ANY (ARRAY['granted'::text, 'replayed'::text])))
  CONSTRAINT chk_bot_reply_outbox_replay_audit_grant_number CHECK ((grant_number > 0))
  CONSTRAINT chk_bot_reply_outbox_replay_audit_reason CHECK ((((octet_length(reason) >= 1) AND (octet_length(reason) <= 256)) AND (reason !~ '[[:cntrl:]]'::text)))
  CONSTRAINT bot_reply_outbox_replay_audit_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES bot_reply_outbox(id) ON DELETE CASCADE
  CONSTRAINT bot_reply_outbox_replay_audit_pkey PRIMARY KEY (id)
  CONSTRAINT bot_reply_outbox_replay_audit_outbox_id_grant_number_event__key UNIQUE (outbox_id, grant_number, event_type)
  INDEX CREATE INDEX idx_bot_reply_outbox_replay_audit_outbox_recorded ON public.bot_reply_outbox_replay_audit USING btree (outbox_id, recorded_at, id)
  TRIGGER CREATE TRIGGER bot_reply_outbox_replay_audit_immutable BEFORE DELETE OR UPDATE ON bot_reply_outbox_replay_audit FOR EACH ROW EXECUTE FUNCTION reject_bot_reply_outbox_replay_audit_mutation()

TABLE bot_webhook_heads
  COLUMN ordering_key text NOT NULL
  COLUMN message_id text NOT NULL
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_bot_webhook_heads_ordering_key_size CHECK (((length(ordering_key) > 0) AND (length(ordering_key) <= 512)))
  CONSTRAINT bot_webhook_heads_message_id_fkey FOREIGN KEY (message_id) REFERENCES bot_webhook_inbox(message_id) ON DELETE CASCADE
  CONSTRAINT bot_webhook_heads_pkey PRIMARY KEY (ordering_key)
  CONSTRAINT bot_webhook_heads_message_id_key UNIQUE (message_id)

TABLE bot_webhook_inbox
  COLUMN id bigint NOT NULL DEFAULT nextval('bot_webhook_inbox_id_seq'::regclass)
  COLUMN message_id text NOT NULL
  COLUMN room_id text NOT NULL
  COLUMN ordering_key text NOT NULL
  COLUMN payload jsonb NOT NULL
  COLUMN status text NOT NULL DEFAULT 'pending'::text
  COLUMN attempts integer NOT NULL DEFAULT 0
  COLUMN claim_token text
  COLUMN lease_until timestamp with time zone
  COLUMN available_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN terminal_reason text NOT NULL DEFAULT ''::text
  COLUMN terminal_at timestamp with time zone
  COLUMN last_error text NOT NULL DEFAULT ''::text
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_bot_webhook_inbox_attempts CHECK ((attempts >= 0))
  CONSTRAINT chk_bot_webhook_inbox_last_error_size CHECK ((octet_length(last_error) <= 8192))
  CONSTRAINT chk_bot_webhook_inbox_message_id_size CHECK (((length(message_id) > 0) AND (length(message_id) <= 512)))
  CONSTRAINT chk_bot_webhook_inbox_ordering_key_size CHECK (((length(ordering_key) > 0) AND (length(ordering_key) <= 512)))
  CONSTRAINT chk_bot_webhook_inbox_room_id_size CHECK (((length(room_id) > 0) AND (length(room_id) <= 256)))
  CONSTRAINT chk_bot_webhook_inbox_state_shape CHECK ((((status <> 'processing'::text) OR ((claim_token IS NOT NULL) AND (lease_until IS NOT NULL))) AND ((status <> 'dead'::text) OR ((terminal_at IS NOT NULL) AND (length(terminal_reason) > 0)))))
  CONSTRAINT chk_bot_webhook_inbox_status_vocab CHECK ((status = ANY (ARRAY['pending'::text, 'processing'::text, 'retry'::text, 'dead'::text, 'succeeded'::text])))
  CONSTRAINT chk_bot_webhook_inbox_terminal_payload_scrubbed CHECK (((status <> ALL (ARRAY['dead'::text, 'succeeded'::text])) OR (payload = '{}'::jsonb)))
  CONSTRAINT chk_bot_webhook_inbox_terminal_reason_size CHECK ((length(terminal_reason) <= 512))
  CONSTRAINT bot_webhook_inbox_pkey PRIMARY KEY (id)
  CONSTRAINT bot_webhook_inbox_message_id_key UNIQUE (message_id)
  INDEX CREATE INDEX idx_bot_webhook_inbox_due ON public.bot_webhook_inbox USING btree (available_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retry'::text]))
  INDEX CREATE INDEX idx_bot_webhook_inbox_lease_expiry ON public.bot_webhook_inbox USING btree (lease_until, id) WHERE (status = 'processing'::text)
  INDEX CREATE INDEX idx_bot_webhook_inbox_ordering_partition ON public.bot_webhook_inbox USING btree (ordering_key, id) WHERE (status = ANY (ARRAY['pending'::text, 'processing'::text, 'retry'::text]))
  INDEX CREATE INDEX idx_bot_webhook_inbox_terminal_updated ON public.bot_webhook_inbox USING btree (updated_at, id) WHERE (status = ANY (ARRAY['dead'::text, 'succeeded'::text]))
  TRIGGER CREATE TRIGGER bot_webhook_inbox_terminal_payload_scrub BEFORE INSERT OR UPDATE OF status, payload ON bot_webhook_inbox FOR EACH ROW WHEN (new.status = ANY (ARRAY['dead'::text, 'succeeded'::text])) EXECUTE FUNCTION scrub_bot_webhook_inbox_terminal_payload()

TABLE kakao_rooms
  COLUMN room_id character varying(100) NOT NULL
  COLUMN room_type character varying(64) NOT NULL DEFAULT ''::character varying
  COLUMN room_link_id character varying(128) NOT NULL DEFAULT ''::character varying
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT kakao_rooms_room_id_len CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100)))
  CONSTRAINT kakao_rooms_room_link_id_len CHECK ((length((room_link_id)::text) <= 128))
  CONSTRAINT kakao_rooms_room_type_len CHECK ((length((room_type)::text) <= 64))
  CONSTRAINT kakao_rooms_pkey PRIMARY KEY (room_id)

TABLE major_event_subscriptions
  COLUMN id integer NOT NULL DEFAULT nextval('major_event_subscriptions_id_seq'::regclass)
  COLUMN room_id character varying(100) NOT NULL
  COLUMN room_name character varying(255)
  COLUMN created_at timestamp with time zone DEFAULT now()
  CONSTRAINT major_event_subscriptions_pkey PRIMARY KEY (id)
  CONSTRAINT major_event_subscriptions_room_id_key UNIQUE (room_id)

TABLE major_events
  COLUMN id integer NOT NULL DEFAULT nextval('major_events_id_seq'::regclass)
  COLUMN external_id character varying(500) NOT NULL
  COLUMN type character varying(20) NOT NULL DEFAULT 'event'::character varying
  COLUMN title character varying(500) NOT NULL
  COLUMN link character varying(1000) NOT NULL
  COLUMN description text
  COLUMN members text[]
  COLUMN pub_date timestamp with time zone
  COLUMN event_start_date date
  COLUMN event_end_date date
  COLUMN status text NOT NULL DEFAULT 'active'::character varying
  COLUMN notified_at timestamp with time zone
  COLUMN notified_week character varying(10)
  COLUMN created_at timestamp with time zone DEFAULT now()
  COLUMN updated_at timestamp with time zone DEFAULT now()
  COLUMN notified_month character varying(10)
  COLUMN link_status character varying(20) NOT NULL DEFAULT 'unchecked'::character varying
  COLUMN link_checked_at timestamp with time zone
  CONSTRAINT chk_major_events_status_vocab CHECK ((status = ANY (ARRAY['active'::text, 'ended'::text, 'canceled'::text])))
  CONSTRAINT major_events_pkey PRIMARY KEY (id)
  CONSTRAINT major_events_external_id_key UNIQUE (external_id)
  INDEX CREATE INDEX idx_major_events_start_date ON public.major_events USING btree (event_start_date)
  INDEX CREATE INDEX idx_major_events_status_type_start ON public.major_events USING btree (status, type, event_start_date)

TABLE member_news_subscriptions
  COLUMN id integer NOT NULL DEFAULT nextval('member_news_subscriptions_id_seq'::regclass)
  COLUMN room_id character varying(100) NOT NULL
  COLUMN room_name character varying(255)
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT member_news_subscriptions_pkey PRIMARY KEY (id)
  CONSTRAINT member_news_subscriptions_room_id_key UNIQUE (room_id)
  INDEX CREATE INDEX idx_member_news_subscriptions_created_at ON public.member_news_subscriptions USING btree (created_at)

TABLE members
  COLUMN id integer NOT NULL DEFAULT nextval('members_id_seq'::regclass)
  COLUMN slug character varying(100) NOT NULL
  COLUMN channel_id character varying(64)
  COLUMN english_name character varying(200) NOT NULL
  COLUMN japanese_name character varying(200)
  COLUMN korean_name character varying(200)
  COLUMN status text NOT NULL DEFAULT 'active'::character varying
  COLUMN is_graduated boolean NOT NULL DEFAULT false
  COLUMN aliases jsonb
  COLUMN photo text
  COLUMN photo_updated_at timestamp with time zone
  COLUMN org character varying(50) NOT NULL
  COLUMN suborg character varying(100)
  COLUMN sync_source character varying(20) NOT NULL
  COLUMN chzzk_channel_id character varying(32)
  COLUMN twitch_user_id character varying(50)
  COLUMN short_korean_name character varying(64)
  COLUMN birthday date
  COLUMN debut_date date
  CONSTRAINT chk_members_graduated_sync CHECK ((is_graduated = (status = 'graduated'::text)))
  CONSTRAINT chk_members_status_vocab CHECK ((status = ANY (ARRAY[('active'::character varying)::text, ('graduated'::character varying)::text])))
  CONSTRAINT members_pkey PRIMARY KEY (id)
  INDEX CREATE INDEX idx_members_active_channel ON public.members USING btree (channel_id) WHERE ((is_graduated = false) AND (channel_id IS NOT NULL))
  INDEX CREATE INDEX idx_members_aliases_ja_gin ON public.members USING gin (((aliases -> 'ja'::text)))
  INDEX CREATE INDEX idx_members_aliases_ko_gin ON public.members USING gin (((aliases -> 'ko'::text)))
  INDEX CREATE INDEX idx_members_birthday_month_day ON public.members USING btree (EXTRACT(month FROM birthday), EXTRACT(day FROM birthday)) WHERE (birthday IS NOT NULL)
  INDEX CREATE INDEX idx_members_channel_id ON public.members USING btree (channel_id) WHERE (channel_id IS NOT NULL)
  INDEX CREATE INDEX idx_members_debut_date_month_day ON public.members USING btree (EXTRACT(month FROM debut_date), EXTRACT(day FROM debut_date)) WHERE (debut_date IS NOT NULL)
  INDEX CREATE INDEX idx_members_english_name ON public.members USING btree (english_name)
  INDEX CREATE INDEX idx_members_org_english_name ON public.members USING btree (org, english_name)
  INDEX CREATE INDEX idx_members_photo_updated_at ON public.members USING btree (photo_updated_at)
  INDEX CREATE UNIQUE INDEX idx_members_slug ON public.members USING btree (slug)

TABLE message_strings
  COLUMN id bigint NOT NULL DEFAULT nextval('message_strings_id_seq'::regclass)
  COLUMN namespace character varying(32) NOT NULL
  COLUMN key character varying(64) NOT NULL
  COLUMN value text NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT message_strings_pkey PRIMARY KEY (id)
  CONSTRAINT ux_message_strings UNIQUE (namespace, key)

TABLE notification_delivery_outbox
  OPTIONS autovacuum_analyze_scale_factor=0.02,autovacuum_analyze_threshold=50,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=50
  COLUMN id bigint NOT NULL DEFAULT nextval('notification_delivery_outbox_id_seq'::regclass)
  COLUMN kind text NOT NULL
  COLUMN period_key character varying(20) NOT NULL
  COLUMN room_id character varying(100) NOT NULL
  COLUMN content_id character varying(200) NOT NULL
  COLUMN payload jsonb NOT NULL DEFAULT '{}'::jsonb
  COLUMN status text NOT NULL DEFAULT 'PENDING'::character varying
  COLUMN attempt_count integer NOT NULL DEFAULT 0
  COLUMN next_attempt_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN locked_at timestamp with time zone
  COLUMN sent_at timestamp with time zone
  COLUMN error text
  COLUMN locked_by text
  COLUMN lock_expires_at timestamp with time zone
  COLUMN sending_started_at timestamp with time zone
  CONSTRAINT chk_notification_delivery_outbox_kind_vocab CHECK ((kind = ANY (ARRAY['MAJOR_EVENT_WEEKLY'::text, 'MAJOR_EVENT_MONTHLY'::text, 'MEMBER_NEWS_WEEKLY'::text, 'MEMBER_NEWS_MONTHLY'::text])))
  CONSTRAINT chk_notification_delivery_outbox_status_vocab CHECK ((status = ANY (ARRAY['PENDING'::text, 'SENDING'::text, 'SENT'::text, 'FAILED'::text, 'QUARANTINED'::text])))
  CONSTRAINT notification_delivery_outbox_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX idx_ndo_kind_content ON public.notification_delivery_outbox USING btree (kind, content_id)
  INDEX CREATE INDEX idx_ndo_pending_due_created_id ON public.notification_delivery_outbox USING btree (next_attempt_at, created_at, id) WHERE (status = 'PENDING'::text)
  INDEX CREATE INDEX idx_ndo_sending_stale ON public.notification_delivery_outbox USING btree (sending_started_at, id) WHERE (status = 'SENDING'::text)
  INDEX CREATE INDEX idx_ndo_terminal_cleanup ON public.notification_delivery_outbox USING btree (COALESCE(sent_at, created_at)) WHERE (status = ANY (ARRAY['SENT'::text, 'FAILED'::text, 'QUARANTINED'::text]))

TABLE notification_template_revisions
  COLUMN id bigint NOT NULL DEFAULT nextval('notification_template_revisions_id_seq'::regclass)
  COLUMN template_id bigint NOT NULL
  COLUMN body text NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT notification_template_revisions_template_id_fkey FOREIGN KEY (template_id) REFERENCES notification_templates(id) ON DELETE CASCADE
  CONSTRAINT notification_template_revisions_pkey PRIMARY KEY (id)
  INDEX CREATE INDEX idx_template_revisions_template_created ON public.notification_template_revisions USING btree (template_id, created_at DESC)

TABLE notification_templates
  COLUMN id bigint NOT NULL DEFAULT nextval('notification_templates_id_seq'::regclass)
  COLUMN template_key character varying(50) NOT NULL
  COLUMN channel_id character varying(64)
  COLUMN body text NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT notification_templates_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX ux_notification_templates_channel ON public.notification_templates USING btree (template_key, channel_id) WHERE (channel_id IS NOT NULL)
  INDEX CREATE UNIQUE INDEX ux_notification_templates_default ON public.notification_templates USING btree (template_key) WHERE (channel_id IS NULL)

TABLE observation_contract_generations
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN current_schema_version smallint NOT NULL
  COLUMN current_generation bigint NOT NULL
  COLUMN updated_by text NOT NULL
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_observation_contract_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'viewer_sample'::text, 'channel_stats'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text])))
  CONSTRAINT chk_observation_contract_provider_vocab CHECK ((provider = ANY (ARRAY['holodex'::text, 'youtubejs'::text, 'hololive_official'::text])))
  CONSTRAINT chk_observation_contract_updated_by CHECK (((length(updated_by) >= 1) AND (length(updated_by) <= 128)))
  CONSTRAINT observation_contract_generations_current_generation_check CHECK ((current_generation > 0))
  CONSTRAINT observation_contract_generations_current_schema_version_check CHECK ((current_schema_version > 0))
  CONSTRAINT observation_contract_generations_pkey PRIMARY KEY (provider, observation_kind)

TABLE source_collection_checkpoints
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN scope_sha256 text NOT NULL
  COLUMN contract_generation bigint NOT NULL
  COLUMN last_observation_key text NOT NULL
  COLUMN last_evidence_sha256 text NOT NULL
  COLUMN last_scheduled_for timestamp with time zone NOT NULL
  COLUMN last_success_at timestamp with time zone NOT NULL
  COLUMN collection_latency_ms bigint NOT NULL
  COLUMN continuity text NOT NULL
  COLUMN cursor jsonb
  COLUMN last_error_code text
  COLUMN last_error_at timestamp with time zone
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_source_checkpoint_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(last_observation_key) >= 1) AND (length(last_observation_key) <= 512)) AND ((last_error_code IS NULL) OR ((length(last_error_code) >= 1) AND (length(last_error_code) <= 128)))))
  CONSTRAINT chk_source_checkpoint_continuity_vocab CHECK ((continuity = ANY (ARRAY['CONTIGUOUS'::text, 'GAP_UNRESOLVED'::text, 'NOT_APPLICABLE'::text])))
  CONSTRAINT chk_source_checkpoint_cursor CHECK (((cursor IS NULL) OR ((jsonb_typeof(cursor) = 'object'::text) AND (octet_length((cursor)::text) <= 16384))))
  CONSTRAINT chk_source_checkpoint_error_shape CHECK (((last_error_code IS NULL) = (last_error_at IS NULL)))
  CONSTRAINT chk_source_checkpoint_hashes CHECK (((scope_sha256 ~ '^[0-9a-f]{64}$'::text) AND (last_evidence_sha256 ~ '^[0-9a-f]{64}$'::text)))
  CONSTRAINT source_collection_checkpoints_collection_latency_ms_check CHECK ((collection_latency_ms >= 0))
  CONSTRAINT source_collection_checkpoints_contract_generation_check CHECK ((contract_generation > 0))
  CONSTRAINT fk_source_checkpoint_contract FOREIGN KEY (provider, observation_kind) REFERENCES observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT
  CONSTRAINT source_collection_checkpoints_pkey PRIMARY KEY (provider, observation_kind, subject_key, scope_sha256)

TABLE source_observation_applications
  COLUMN id bigint NOT NULL DEFAULT nextval('source_observation_applications_id_seq'::regclass)
  COLUMN observation_id bigint
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN evidence_sha256 text NOT NULL
  COLUMN entity_kind text NOT NULL
  COLUMN entity_key text NOT NULL
  COLUMN decision text NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN applied_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_source_observation_application_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(entity_kind) >= 1) AND (length(entity_kind) <= 64)) AND ((length(entity_key) >= 1) AND (length(entity_key) <= 256)) AND ((length(decision) >= 1) AND (length(decision) <= 128))))
  CONSTRAINT chk_source_observation_application_hash CHECK ((evidence_sha256 ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT fk_source_observation_application_contract FOREIGN KEY (provider, observation_kind) REFERENCES observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT
  CONSTRAINT source_observation_applications_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT source_observation_applications_pkey PRIMARY KEY (id)
  CONSTRAINT uq_source_observation_application UNIQUE (observation_id, entity_kind, entity_key)

TABLE source_observation_collisions
  COLUMN id bigint NOT NULL DEFAULT nextval('source_observation_collisions_id_seq'::regclass)
  COLUMN existing_observation_id bigint
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN observation_key text NOT NULL
  COLUMN schema_version smallint NOT NULL
  COLUMN contract_generation bigint NOT NULL
  COLUMN existing_evidence_sha256 text NOT NULL
  COLUMN attempted_evidence_sha256 text NOT NULL
  COLUMN attempted_payload_sha256 text NOT NULL
  COLUMN collector_instance text NOT NULL
  COLUMN job_key text NOT NULL
  COLUMN fence_epoch bigint NOT NULL
  COLUMN occurred_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_source_observation_collision_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(observation_key) >= 1) AND (length(observation_key) <= 512)) AND ((length(collector_instance) >= 1) AND (length(collector_instance) <= 128)) AND ((length(job_key) >= 1) AND (length(job_key) <= 512))))
  CONSTRAINT chk_source_observation_collision_hashes CHECK (((existing_evidence_sha256 ~ '^[0-9a-f]{64}$'::text) AND (attempted_evidence_sha256 ~ '^[0-9a-f]{64}$'::text) AND (attempted_payload_sha256 ~ '^[0-9a-f]{64}$'::text)))
  CONSTRAINT source_observation_collisions_contract_generation_check CHECK ((contract_generation > 0))
  CONSTRAINT source_observation_collisions_fence_epoch_check CHECK ((fence_epoch > 0))
  CONSTRAINT source_observation_collisions_schema_version_check CHECK ((schema_version > 0))
  CONSTRAINT fk_source_observation_collision_contract FOREIGN KEY (provider, observation_kind) REFERENCES observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT
  CONSTRAINT source_observation_collisions_existing_observation_id_fkey FOREIGN KEY (existing_observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT source_observation_collisions_pkey PRIMARY KEY (id)
  INDEX CREATE INDEX idx_source_observation_collisions_existing_observation ON public.source_observation_collisions USING btree (existing_observation_id)
  INDEX CREATE INDEX idx_source_observation_collisions_occurred ON public.source_observation_collisions USING btree (occurred_at, id)

TABLE source_observation_consumer_offsets
  COLUMN consumer_name text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN last_processed_id bigint NOT NULL DEFAULT 0
  COLUMN last_effective_at timestamp with time zone
  COLUMN last_processed_at timestamp with time zone
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_source_observation_consumer_offset_bounds CHECK (((length(consumer_name) >= 1) AND (length(consumer_name) <= 128)))
  CONSTRAINT chk_source_observation_consumer_offset_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'viewer_sample'::text, 'channel_stats'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text])))
  CONSTRAINT source_observation_consumer_offsets_last_processed_id_check CHECK ((last_processed_id >= 0))
  CONSTRAINT source_observation_consumer_offsets_pkey PRIMARY KEY (consumer_name, observation_kind)

TABLE source_observation_queue
  COLUMN observation_id bigint NOT NULL
  COLUMN status text NOT NULL DEFAULT 'PENDING'::text
  COLUMN attempt_count smallint NOT NULL DEFAULT 0
  COLUMN replay_count smallint NOT NULL DEFAULT 0
  COLUMN available_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN lease_owner text
  COLUMN lease_token text
  COLUMN lease_expires_at timestamp with time zone
  COLUMN processed_at timestamp with time zone
  COLUMN dead_lettered_at timestamp with time zone
  COLUMN last_error_code text
  COLUMN last_error_detail text
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_source_observation_queue_bounds CHECK ((((lease_owner IS NULL) OR ((length(lease_owner) >= 1) AND (length(lease_owner) <= 128))) AND ((lease_token IS NULL) OR (lease_token ~ '^[0-9a-f]{64}$'::text)) AND ((last_error_code IS NULL) OR ((length(last_error_code) >= 1) AND (length(last_error_code) <= 128))) AND ((last_error_detail IS NULL) OR (length(last_error_detail) <= 2048))))
  CONSTRAINT chk_source_observation_queue_lease_shape CHECK ((((status = 'PROCESSING'::text) AND (lease_owner IS NOT NULL) AND (lease_token IS NOT NULL) AND (lease_expires_at IS NOT NULL)) OR ((status <> 'PROCESSING'::text) AND (lease_owner IS NULL) AND (lease_token IS NULL) AND (lease_expires_at IS NULL))))
  CONSTRAINT chk_source_observation_queue_status_vocab CHECK ((status = ANY (ARRAY['PENDING'::text, 'PROCESSING'::text, 'PROCESSED'::text, 'DEAD_LETTER'::text])))
  CONSTRAINT chk_source_observation_queue_terminal_shape CHECK ((((status = 'PROCESSED'::text) = (processed_at IS NOT NULL)) AND ((status = 'DEAD_LETTER'::text) = (dead_lettered_at IS NOT NULL))))
  CONSTRAINT source_observation_queue_attempt_count_check CHECK (((attempt_count >= 0) AND (attempt_count <= 64)))
  CONSTRAINT source_observation_queue_replay_count_check CHECK (((replay_count >= 0) AND (replay_count <= 16)))
  CONSTRAINT source_observation_queue_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE CASCADE
  CONSTRAINT source_observation_queue_pkey PRIMARY KEY (observation_id)
  INDEX CREATE INDEX idx_source_observation_queue_claim ON public.source_observation_queue USING btree (available_at, observation_id) WHERE (status = 'PENDING'::text)
  INDEX CREATE INDEX idx_source_observation_queue_lease_recovery ON public.source_observation_queue USING btree (lease_expires_at, observation_id) WHERE (status = 'PROCESSING'::text)
  INDEX CREATE INDEX idx_source_observation_queue_terminal_retention ON public.source_observation_queue USING btree (status, updated_at, observation_id) WHERE (status = ANY (ARRAY['PROCESSED'::text, 'DEAD_LETTER'::text]))

TABLE source_observation_replay_requests
  COLUMN id bigint NOT NULL DEFAULT nextval('source_observation_replay_requests_id_seq'::regclass)
  COLUMN observation_id bigint
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN observation_key text NOT NULL
  COLUMN evidence_sha256 text NOT NULL
  COLUMN requested_by text NOT NULL
  COLUMN reason text NOT NULL
  COLUMN previous_attempt_count smallint NOT NULL
  COLUMN status text NOT NULL DEFAULT 'PENDING'::text
  COLUMN requested_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN applied_at timestamp with time zone
  COLUMN rejection_code text
  CONSTRAINT chk_source_observation_replay_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(observation_key) >= 1) AND (length(observation_key) <= 512)) AND ((length(requested_by) >= 1) AND (length(requested_by) <= 128)) AND ((length(reason) >= 1) AND (length(reason) <= 1024)) AND ((rejection_code IS NULL) OR ((length(rejection_code) >= 1) AND (length(rejection_code) <= 128)))))
  CONSTRAINT chk_source_observation_replay_hash CHECK ((evidence_sha256 ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT chk_source_observation_replay_terminal_shape CHECK ((((status = 'APPLIED'::text) AND (applied_at IS NOT NULL) AND (rejection_code IS NULL)) OR ((status = 'REJECTED'::text) AND (applied_at IS NULL) AND (rejection_code IS NOT NULL)) OR ((status = 'PENDING'::text) AND (applied_at IS NULL) AND (rejection_code IS NULL))))
  CONSTRAINT source_observation_replay_requests_previous_attempt_count_check CHECK (((previous_attempt_count >= 0) AND (previous_attempt_count <= 64)))
  CONSTRAINT source_observation_replay_requests_status_check CHECK ((status = ANY (ARRAY['PENDING'::text, 'APPLIED'::text, 'REJECTED'::text])))
  CONSTRAINT fk_source_observation_replay_contract FOREIGN KEY (provider, observation_kind) REFERENCES observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT
  CONSTRAINT source_observation_replay_requests_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT source_observation_replay_requests_pkey PRIMARY KEY (id)
  INDEX CREATE INDEX idx_source_observation_replay_observation_status ON public.source_observation_replay_requests USING btree (observation_id, status)
  INDEX CREATE INDEX idx_source_observation_replay_pending ON public.source_observation_replay_requests USING btree (requested_at, id) WHERE (status = 'PENDING'::text)

TABLE source_observation_subject_heads
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN source_observation_id bigint NOT NULL
  COLUMN evidence_sha256 text NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_source_observation_subject_head_bounds CHECK (((length(subject_key) >= 1) AND (length(subject_key) <= 256)))
  CONSTRAINT chk_source_observation_subject_head_hash CHECK ((evidence_sha256 ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT fk_source_observation_subject_head_contract FOREIGN KEY (provider, observation_kind) REFERENCES observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT
  CONSTRAINT source_observation_subject_heads_pkey PRIMARY KEY (provider, observation_kind, subject_key)

TABLE source_observations
  COLUMN id bigint NOT NULL DEFAULT nextval('source_observations_id_seq'::regclass)
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN observation_key text NOT NULL
  COLUMN schema_version smallint NOT NULL
  COLUMN contract_generation bigint NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN observed_at timestamp with time zone NOT NULL
  COLUMN source_event_at timestamp with time zone
  COLUMN received_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN scope_sha256 text NOT NULL
  COLUMN completeness text NOT NULL
  COLUMN continuity text NOT NULL
  COLUMN payload jsonb NOT NULL
  COLUMN payload_sha256 text NOT NULL
  COLUMN evidence_sha256 text NOT NULL
  COLUMN collector_instance text NOT NULL
  COLUMN job_key text NOT NULL
  COLUMN collection_job_kind text NOT NULL
  COLUMN fence_epoch bigint NOT NULL
  COLUMN projection_generation bigint NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_source_observation_completeness_vocab CHECK ((completeness = ANY (ARRAY['COMPLETE'::text, 'PARTIAL'::text, 'UNKNOWN'::text])))
  CONSTRAINT chk_source_observation_continuity_vocab CHECK ((continuity = ANY (ARRAY['CONTIGUOUS'::text, 'GAP_UNRESOLVED'::text, 'NOT_APPLICABLE'::text])))
  CONSTRAINT chk_source_observation_hashes CHECK (((scope_sha256 ~ '^[0-9a-f]{64}$'::text) AND (payload_sha256 ~ '^[0-9a-f]{64}$'::text) AND (evidence_sha256 ~ '^[0-9a-f]{64}$'::text)))
  CONSTRAINT chk_source_observation_payload CHECK (((jsonb_typeof(payload) = 'object'::text) AND (octet_length((payload)::text) <= 1048576)))
  CONSTRAINT chk_source_observation_text_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(observation_key) >= 1) AND (length(observation_key) <= 512)) AND ((length(collector_instance) >= 1) AND (length(collector_instance) <= 128)) AND ((length(job_key) >= 1) AND (length(job_key) <= 512)) AND ((length(collection_job_kind) >= 1) AND (length(collection_job_kind) <= 128))))
  CONSTRAINT source_observations_contract_generation_check CHECK ((contract_generation > 0))
  CONSTRAINT source_observations_fence_epoch_check CHECK ((fence_epoch > 0))
  CONSTRAINT source_observations_projection_generation_check CHECK ((projection_generation > 0))
  CONSTRAINT source_observations_schema_version_check CHECK ((schema_version > 0))
  CONSTRAINT fk_source_observation_contract FOREIGN KEY (provider, observation_kind) REFERENCES observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT
  CONSTRAINT source_observations_pkey PRIMARY KEY (id)
  CONSTRAINT uq_source_observation_identity UNIQUE (provider, observation_kind, subject_key, observation_key, schema_version, contract_generation)
  INDEX CREATE INDEX idx_source_observations_kind_id ON public.source_observations USING btree (observation_kind, id)
  INDEX CREATE INDEX idx_source_observations_kind_received_id ON public.source_observations USING btree (observation_kind, received_at, id)
  INDEX CREATE INDEX idx_source_observations_received ON public.source_observations USING btree (received_at, id)
  INDEX CREATE INDEX idx_source_observations_subject_time ON public.source_observations USING btree (observation_kind, subject_key, scheduled_for DESC, id DESC)

TABLE source_reconciliation_conflicts
  COLUMN id bigint NOT NULL DEFAULT nextval('source_reconciliation_conflicts_id_seq'::regclass)
  COLUMN observation_id bigint
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN observation_key text NOT NULL
  COLUMN evidence_sha256 text NOT NULL
  COLUMN entity_kind text NOT NULL
  COLUMN entity_key text NOT NULL
  COLUMN field_name text NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN existing_value_sha256 text NOT NULL
  COLUMN attempted_value_sha256 text NOT NULL
  COLUMN decision text NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_source_reconciliation_conflict_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(observation_key) >= 1) AND (length(observation_key) <= 512)) AND ((length(entity_kind) >= 1) AND (length(entity_kind) <= 64)) AND ((length(entity_key) >= 1) AND (length(entity_key) <= 256)) AND ((length(field_name) >= 1) AND (length(field_name) <= 128))))
  CONSTRAINT chk_source_reconciliation_conflict_hashes CHECK (((evidence_sha256 ~ '^[0-9a-f]{64}$'::text) AND (existing_value_sha256 ~ '^[0-9a-f]{64}$'::text) AND (attempted_value_sha256 ~ '^[0-9a-f]{64}$'::text)))
  CONSTRAINT source_reconciliation_conflicts_decision_check CHECK ((decision = ANY (ARRAY['KEEP_EXISTING'::text, 'UNRESOLVED'::text])))
  CONSTRAINT fk_source_reconciliation_conflict_contract FOREIGN KEY (provider, observation_kind) REFERENCES observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT
  CONSTRAINT source_reconciliation_conflicts_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT source_reconciliation_conflicts_pkey PRIMARY KEY (id)
  CONSTRAINT uq_source_reconciliation_conflict UNIQUE (observation_id, entity_kind, entity_key, field_name)

TABLE youtube_channel_latest_stats
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN member_name text
  COLUMN subscribers bigint
  COLUMN videos bigint
  COLUMN views bigint
  COLUMN time timestamp with time zone NOT NULL
  COLUMN updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
  CONSTRAINT youtube_channel_latest_stats_pkey PRIMARY KEY (channel_id)

TABLE youtube_channel_photo_heads
  COLUMN channel_id text NOT NULL
  COLUMN kind text NOT NULL
  COLUMN identity text NOT NULL DEFAULT ''::text
  COLUMN url text NOT NULL DEFAULT ''::text
  COLUMN width integer NOT NULL DEFAULT 0
  COLUMN height integer NOT NULL DEFAULT 0
  COLUMN effective_at timestamp with time zone
  COLUMN candidate_identity text NOT NULL DEFAULT ''::text
  COLUMN candidate_url text NOT NULL DEFAULT ''::text
  COLUMN candidate_width integer NOT NULL DEFAULT 0
  COLUMN candidate_height integer NOT NULL DEFAULT 0
  COLUMN candidate_slots smallint NOT NULL DEFAULT 0
  COLUMN candidate_first_scheduled_for timestamp with time zone
  COLUMN candidate_last_scheduled_for timestamp with time zone
  COLUMN candidate_first_received_at timestamp with time zone
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_photo_head_bounds CHECK (((length(identity) <= 520) AND (length(url) <= 2048) AND (length(candidate_identity) <= 520) AND (length(candidate_url) <= 2048) AND ((width >= 0) AND (width <= 20000)) AND ((height >= 0) AND (height <= 20000)) AND ((candidate_width >= 0) AND (candidate_width <= 20000)) AND ((candidate_height >= 0) AND (candidate_height <= 20000)) AND ((candidate_slots >= 0) AND (candidate_slots <= 32767))))
  CONSTRAINT chk_youtube_photo_head_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64)))
  CONSTRAINT chk_youtube_photo_head_kind CHECK ((kind = ANY (ARRAY['avatar'::text, 'banner'::text])))
  CONSTRAINT youtube_channel_photo_heads_pkey PRIMARY KEY (channel_id, kind)

TABLE youtube_channel_photo_variants
  COLUMN channel_id text NOT NULL
  COLUMN kind text NOT NULL
  COLUMN provider text NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN url text NOT NULL
  COLUMN width integer NOT NULL DEFAULT 0
  COLUMN height integer NOT NULL DEFAULT 0
  COLUMN stable_media_id text NOT NULL DEFAULT ''::text
  COLUMN content_fingerprint text NOT NULL DEFAULT ''::text
  COLUMN observation_id bigint
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  CONSTRAINT chk_youtube_photo_variant_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64)))
  CONSTRAINT chk_youtube_photo_variant_dims CHECK ((((width >= 0) AND (width <= 20000)) AND ((height >= 0) AND (height <= 20000))))
  CONSTRAINT chk_youtube_photo_variant_identity CHECK (((length(stable_media_id) <= 512) AND ((content_fingerprint = ''::text) OR (content_fingerprint ~ '^[0-9a-f]{64}$'::text))))
  CONSTRAINT chk_youtube_photo_variant_kind CHECK ((kind = ANY (ARRAY['avatar'::text, 'banner'::text])))
  CONSTRAINT chk_youtube_photo_variant_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text])))
  CONSTRAINT chk_youtube_photo_variant_url CHECK ((((length(url) >= 8) AND (length(url) <= 2048)) AND (url ~~ 'https://%'::text)))
  CONSTRAINT youtube_channel_photo_variants_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT youtube_channel_photo_variants_pkey PRIMARY KEY (channel_id, kind, provider, scheduled_for)
  INDEX CREATE INDEX idx_youtube_channel_photo_variants_observation_id ON public.youtube_channel_photo_variants USING btree (observation_id)

TABLE youtube_channel_profile_evidence
  COLUMN channel_id text NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN provider text NOT NULL
  COLUMN observation_id bigint
  COLUMN handle_present boolean NOT NULL
  COLUMN handle text NOT NULL DEFAULT ''::text
  COLUMN description_present boolean NOT NULL
  COLUMN description text NOT NULL DEFAULT ''::text
  COLUMN country_present boolean NOT NULL
  COLUMN country text NOT NULL DEFAULT ''::text
  COLUMN joined_date_present boolean NOT NULL
  COLUMN joined_date text NOT NULL DEFAULT ''::text
  COLUMN complete boolean NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  CONSTRAINT chk_youtube_profile_evidence_bounds CHECK (((length(handle) <= 256) AND (octet_length(description) <= 4096) AND (length(country) <= 50) AND (length(joined_date) <= 256)))
  CONSTRAINT chk_youtube_profile_evidence_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64)))
  CONSTRAINT chk_youtube_profile_evidence_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text])))
  CONSTRAINT youtube_channel_profile_evidence_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT youtube_channel_profile_evidence_pkey PRIMARY KEY (channel_id, scheduled_for, provider)
  INDEX CREATE INDEX idx_youtube_channel_profile_evidence_observation_id ON public.youtube_channel_profile_evidence USING btree (observation_id)

TABLE youtube_channel_profile_heads
  COLUMN channel_id text NOT NULL
  COLUMN handle_set boolean NOT NULL DEFAULT false
  COLUMN handle text NOT NULL DEFAULT ''::text
  COLUMN handle_effective_at timestamp with time zone
  COLUMN description_set boolean NOT NULL DEFAULT false
  COLUMN description text NOT NULL DEFAULT ''::text
  COLUMN description_effective_at timestamp with time zone
  COLUMN description_empty_slots smallint NOT NULL DEFAULT 0
  COLUMN description_empty_first_scheduled_for timestamp with time zone
  COLUMN description_empty_last_scheduled_for timestamp with time zone
  COLUMN description_empty_first_received_at timestamp with time zone
  COLUMN country_set boolean NOT NULL DEFAULT false
  COLUMN country text NOT NULL DEFAULT ''::text
  COLUMN country_effective_at timestamp with time zone
  COLUMN country_empty_slots smallint NOT NULL DEFAULT 0
  COLUMN country_empty_first_scheduled_for timestamp with time zone
  COLUMN country_empty_last_scheduled_for timestamp with time zone
  COLUMN country_empty_first_received_at timestamp with time zone
  COLUMN joined_date_set boolean NOT NULL DEFAULT false
  COLUMN joined_date text NOT NULL DEFAULT ''::text
  COLUMN joined_date_effective_at timestamp with time zone
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_profile_head_bounds CHECK (((length(handle) <= 256) AND (octet_length(description) <= 4096) AND (length(country) <= 50) AND (length(joined_date) <= 256) AND ((description_empty_slots >= 0) AND (description_empty_slots <= 32767)) AND ((country_empty_slots >= 0) AND (country_empty_slots <= 32767))))
  CONSTRAINT chk_youtube_profile_head_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64)))
  CONSTRAINT youtube_channel_profile_heads_pkey PRIMARY KEY (channel_id)

TABLE youtube_channel_profiles
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN avatar jsonb
  COLUMN banner jsonb
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT youtube_channel_profiles_pkey PRIMARY KEY (channel_id)

TABLE youtube_channel_stats_evidence
  COLUMN channel_id text NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN provider text NOT NULL
  COLUMN observation_id bigint
  COLUMN subscriber_count bigint
  COLUMN view_count bigint
  COLUMN video_count bigint
  COLUMN subscriber_covered boolean NOT NULL
  COLUMN view_covered boolean NOT NULL
  COLUMN video_covered boolean NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  CONSTRAINT chk_youtube_stats_evidence_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64)))
  CONSTRAINT chk_youtube_stats_evidence_counts CHECK ((((subscriber_count IS NULL) OR (subscriber_count >= 0)) AND ((view_count IS NULL) OR (view_count >= 0)) AND ((video_count IS NULL) OR (video_count >= 0))))
  CONSTRAINT chk_youtube_stats_evidence_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text])))
  CONSTRAINT youtube_channel_stats_evidence_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT youtube_channel_stats_evidence_pkey PRIMARY KEY (channel_id, scheduled_for, provider)
  INDEX CREATE INDEX idx_youtube_channel_stats_evidence_observation_id ON public.youtube_channel_stats_evidence USING btree (observation_id)

TABLE youtube_channel_stats_heads
  COLUMN channel_id text NOT NULL
  COLUMN last_resolved_scheduled_for timestamp with time zone
  COLUMN last_resolved_subscriber_count bigint
  COLUMN last_resolved_view_count bigint
  COLUMN last_resolved_video_count bigint
  COLUMN prior_resolved_scheduled_for timestamp with time zone
  COLUMN prior_resolved_subscriber_count bigint
  COLUMN prior_resolved_view_count bigint
  COLUMN prior_resolved_video_count bigint
  COLUMN unresolved_scheduled_for timestamp with time zone
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_stats_head_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64)))
  CONSTRAINT chk_youtube_stats_head_counts CHECK ((((last_resolved_subscriber_count IS NULL) OR (last_resolved_subscriber_count >= 0)) AND ((last_resolved_view_count IS NULL) OR (last_resolved_view_count >= 0)) AND ((last_resolved_video_count IS NULL) OR (last_resolved_video_count >= 0)) AND ((prior_resolved_subscriber_count IS NULL) OR (prior_resolved_subscriber_count >= 0)) AND ((prior_resolved_view_count IS NULL) OR (prior_resolved_view_count >= 0)) AND ((prior_resolved_video_count IS NULL) OR (prior_resolved_video_count >= 0))))
  CONSTRAINT youtube_channel_stats_heads_pkey PRIMARY KEY (channel_id)

TABLE youtube_channel_stats_snapshots
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN captured_at timestamp with time zone NOT NULL
  COLUMN subscriber_count bigint
  COLUMN view_count bigint
  COLUMN video_count bigint
  COLUMN joined_date bigint
  COLUMN description text
  COLUMN country character varying(50)
  COLUMN handle character varying(100)
  CONSTRAINT chk_ycss_counts_nonneg CHECK ((((subscriber_count IS NULL) OR (subscriber_count >= 0)) AND ((view_count IS NULL) OR (view_count >= 0)) AND ((video_count IS NULL) OR (video_count >= 0))))
  CONSTRAINT youtube_channel_stats_snapshots_pkey PRIMARY KEY (channel_id, captured_at)
  INDEX CREATE INDEX idx_ycss_captured_at_brin ON public.youtube_channel_stats_snapshots USING brin (captured_at)

TABLE youtube_collection_job_leases
  COLUMN job_key text NOT NULL
  COLUMN provider text NOT NULL
  COLUMN job_class text NOT NULL
  COLUMN collection_job_kind text NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN projection_generation bigint NOT NULL
  COLUMN poll_interval_ms bigint NOT NULL
  COLUMN slot_state text NOT NULL DEFAULT 'IDLE'::text
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN next_due_at timestamp with time zone NOT NULL
  COLUMN retry_not_before timestamp with time zone
  COLUMN fence_epoch bigint NOT NULL DEFAULT 0
  COLUMN owner_instance text
  COLUMN lease_expires_at timestamp with time zone
  COLUMN last_completed_at timestamp with time zone
  COLUMN last_error_code text
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_collection_job_identity CHECK ((((length(job_key) >= 1) AND (length(job_key) <= 512)) AND ((length(collection_job_kind) >= 1) AND (length(collection_job_kind) <= 128)) AND ((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((owner_instance IS NULL) OR ((length(owner_instance) >= 1) AND (length(owner_instance) <= 128))) AND ((last_error_code IS NULL) OR ((length(last_error_code) >= 1) AND (length(last_error_code) <= 128)))))
  CONSTRAINT chk_youtube_collection_job_provider_vocab CHECK ((provider = ANY (ARRAY['holodex'::text, 'youtubejs'::text, 'hololive_official'::text])))
  CONSTRAINT chk_youtube_collection_job_slot_shape CHECK ((((slot_state = 'IDLE'::text) AND (owner_instance IS NULL) AND (lease_expires_at IS NULL) AND (retry_not_before IS NULL)) OR ((slot_state = 'ACTIVE'::text) AND (owner_instance IS NOT NULL) AND (lease_expires_at IS NOT NULL) AND (retry_not_before IS NULL)) OR ((slot_state = 'DEFERRED'::text) AND (owner_instance IS NULL) AND (lease_expires_at IS NULL) AND (retry_not_before IS NOT NULL))))
  CONSTRAINT youtube_collection_job_leases_fence_epoch_check CHECK ((fence_epoch >= 0))
  CONSTRAINT youtube_collection_job_leases_job_class_check CHECK ((job_class = ANY (ARRAY['GLOBAL'::text, 'SUBJECT'::text])))
  CONSTRAINT youtube_collection_job_leases_poll_interval_ms_check CHECK (((poll_interval_ms >= 1000) AND (poll_interval_ms <= 86400000)))
  CONSTRAINT youtube_collection_job_leases_slot_state_check CHECK ((slot_state = ANY (ARRAY['IDLE'::text, 'ACTIVE'::text, 'DEFERRED'::text])))
  CONSTRAINT youtube_collection_job_leases_projection_generation_fkey FOREIGN KEY (projection_generation) REFERENCES youtube_collection_projection_generations(generation) ON DELETE RESTRICT
  CONSTRAINT youtube_collection_job_leases_pkey PRIMARY KEY (job_key)
  INDEX CREATE INDEX idx_youtube_collection_job_due ON public.youtube_collection_job_leases USING btree (slot_state, next_due_at, retry_not_before, lease_expires_at, job_key)
  INDEX CREATE INDEX idx_youtube_collection_job_projection_generation ON public.youtube_collection_job_leases USING btree (projection_generation, job_key)

TABLE youtube_collection_projection_generations
  COLUMN generation bigint NOT NULL GENERATED ALWAYS AS IDENTITY
  COLUMN status text NOT NULL
  COLUMN row_count integer NOT NULL
  COLUMN projection_sha256 text NOT NULL
  COLUMN valid_until timestamp with time zone NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN activated_at timestamp with time zone
  CONSTRAINT chk_youtube_collection_projection_activation_shape CHECK ((((status = 'STAGING'::text) AND (activated_at IS NULL)) OR ((status = ANY (ARRAY['CURRENT'::text, 'RETIRED'::text])) AND (activated_at IS NOT NULL))))
  CONSTRAINT youtube_collection_projection_generatio_projection_sha256_check CHECK ((projection_sha256 ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT youtube_collection_projection_generations_row_count_check CHECK ((row_count >= 0))
  CONSTRAINT youtube_collection_projection_generations_status_check CHECK ((status = ANY (ARRAY['STAGING'::text, 'CURRENT'::text, 'RETIRED'::text])))
  CONSTRAINT youtube_collection_projection_generations_pkey PRIMARY KEY (generation)
  INDEX CREATE INDEX idx_youtube_collection_projection_retired_retention ON public.youtube_collection_projection_generations USING btree (valid_until, generation) WHERE (status = 'RETIRED'::text)
  INDEX CREATE UNIQUE INDEX uq_youtube_collection_projection_one_current ON public.youtube_collection_projection_generations USING btree (status) WHERE (status = 'CURRENT'::text)

TABLE youtube_collection_target_reasons
  COLUMN projection_generation bigint NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN reason_kind text NOT NULL
  COLUMN reason_key text NOT NULL
  CONSTRAINT chk_youtube_collection_target_reason_bounds CHECK ((((length(reason_kind) >= 1) AND (length(reason_kind) <= 128)) AND ((length(reason_key) >= 1) AND (length(reason_key) <= 512))))
  CONSTRAINT youtube_collection_target_rea_projection_generation_subjec_fkey FOREIGN KEY (projection_generation, subject_key, observation_kind) REFERENCES youtube_collection_targets(projection_generation, subject_key, observation_kind) ON DELETE CASCADE
  CONSTRAINT youtube_collection_target_reasons_pkey PRIMARY KEY (projection_generation, subject_key, observation_kind, reason_kind, reason_key)

TABLE youtube_collection_targets
  COLUMN projection_generation bigint NOT NULL
  COLUMN subject_key text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN priority smallint NOT NULL
  COLUMN poll_interval_ms bigint NOT NULL
  COLUMN enabled boolean NOT NULL
  COLUMN valid_until timestamp with time zone NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_collection_target_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'viewer_sample'::text, 'channel_stats'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text])))
  CONSTRAINT chk_youtube_collection_target_subject CHECK (((length(subject_key) >= 1) AND (length(subject_key) <= 256)))
  CONSTRAINT youtube_collection_targets_poll_interval_ms_check CHECK (((poll_interval_ms >= 1000) AND (poll_interval_ms <= 86400000)))
  CONSTRAINT youtube_collection_targets_priority_check CHECK (((priority >= 0) AND (priority <= 100)))
  CONSTRAINT youtube_collection_targets_projection_generation_fkey FOREIGN KEY (projection_generation) REFERENCES youtube_collection_projection_generations(generation) ON DELETE CASCADE
  CONSTRAINT youtube_collection_targets_pkey PRIMARY KEY (projection_generation, subject_key, observation_kind)

TABLE youtube_community_posts
  COLUMN post_id character varying(50) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN author_name character varying(200)
  COLUMN author_photo jsonb
  COLUMN content_text text
  COLUMN published_text character varying(100)
  COLUMN like_count bigint DEFAULT 0
  COLUMN comment_count bigint DEFAULT 0
  COLUMN images jsonb
  COLUMN attached_video character varying(20)
  COLUMN first_seen_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN last_seen_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN published_at timestamp with time zone
  CONSTRAINT youtube_community_posts_pkey PRIMARY KEY (post_id)
  INDEX CREATE INDEX idx_ycp_channel_first_seen ON public.youtube_community_posts USING btree (channel_id, first_seen_at DESC)

TABLE youtube_community_shorts_alarm_states
  OPTIONS autovacuum_analyze_scale_factor=0.02,autovacuum_analyze_threshold=50,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=50
  COLUMN kind text NOT NULL
  COLUMN post_id character varying(50) NOT NULL
  COLUMN content_id character varying(50) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN actual_published_at timestamp with time zone
  COLUMN detected_at timestamp with time zone NOT NULL
  COLUMN authorized_at timestamp with time zone
  COLUMN alarm_sent_at timestamp with time zone
  COLUMN delivery_status text NOT NULL DEFAULT 'DETECTED'::character varying
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab CHECK ((delivery_status = ANY (ARRAY[('DETECTED'::character varying)::text, ('ENQUEUED'::character varying)::text, ('SENT'::character varying)::text])))
  CONSTRAINT chk_youtube_community_shorts_alarm_states_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text, 'MILESTONE'::text])))
  CONSTRAINT youtube_community_shorts_alarm_states_pkey PRIMARY KEY (kind, post_id)
  INDEX CREATE INDEX idx_ycsas_authorized_at ON public.youtube_community_shorts_alarm_states USING btree (authorized_at DESC) WHERE (authorized_at IS NOT NULL)
  INDEX CREATE INDEX idx_ycsas_delivery_status ON public.youtube_community_shorts_alarm_states USING btree (delivery_status, detected_at DESC)
  INDEX CREATE INDEX idx_ycsas_detected_at ON public.youtube_community_shorts_alarm_states USING btree (detected_at DESC)
  INDEX CREATE UNIQUE INDEX idx_ycsas_kind_content ON public.youtube_community_shorts_alarm_states USING btree (kind, content_id)

TABLE youtube_community_shorts_source_posts
  COLUMN kind text NOT NULL
  COLUMN post_id character varying(50) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN actual_published_at timestamp with time zone
  COLUMN detected_at timestamp with time zone NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_community_shorts_source_posts_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text, 'MILESTONE'::text])))
  CONSTRAINT youtube_community_shorts_source_posts_pkey PRIMARY KEY (kind, post_id)
  INDEX CREATE INDEX idx_ycssp_channel_detected ON public.youtube_community_shorts_source_posts USING btree (channel_id, detected_at DESC)

TABLE youtube_content_absence_slots
  COLUMN channel_id character varying(50) NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN observation_id bigint
  COLUMN evidence_sha256 text NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  COLUMN scope_sha256 text NOT NULL
  COLUMN coverage jsonb NOT NULL
  CONSTRAINT chk_youtube_content_absence_bounds CHECK (((length((channel_id)::text) >= 1) AND (length((channel_id)::text) <= 50)))
  CONSTRAINT chk_youtube_content_absence_coverage CHECK (((jsonb_typeof(coverage) = 'object'::text) AND (octet_length((coverage)::text) <= 8192)))
  CONSTRAINT chk_youtube_content_absence_hashes CHECK (((evidence_sha256 ~ '^[0-9a-f]{64}$'::text) AND (scope_sha256 ~ '^[0-9a-f]{64}$'::text)))
  CONSTRAINT chk_youtube_content_absence_kind CHECK ((observation_kind = ANY (ARRAY['video_list'::text, 'shorts_list'::text])))
  CONSTRAINT youtube_content_absence_slots_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT youtube_content_absence_slots_pkey PRIMARY KEY (channel_id, observation_kind, scheduled_for)
  INDEX CREATE INDEX idx_youtube_content_absence_slots_observation_id ON public.youtube_content_absence_slots USING btree (observation_id)

TABLE youtube_content_alarm_tracking
  OPTIONS autovacuum_analyze_scale_factor=0.05,autovacuum_analyze_threshold=100,autovacuum_vacuum_scale_factor=0.05,autovacuum_vacuum_threshold=100
  COLUMN kind text NOT NULL
  COLUMN content_id character varying(50) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN actual_published_at timestamp with time zone
  COLUMN detected_at timestamp with time zone NOT NULL
  COLUMN alarm_sent_at timestamp with time zone
  COLUMN alarm_latency_millis bigint
  COLUMN alarm_latency_exceeded boolean
  COLUMN delivery_status text NOT NULL DEFAULT 'PENDING'::character varying
  COLUMN latency_classification_status character varying(40)
  COLUMN delay_source character varying(40)
  COLUMN internal_delay_cause character varying(40)
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN canonical_content_id character varying(50) NOT NULL
  CONSTRAINT chk_youtube_content_alarm_tracking_delivery_status_vocab CHECK ((delivery_status = ANY (ARRAY[('PENDING'::character varying)::text, ('SENT'::character varying)::text])))
  CONSTRAINT chk_youtube_content_alarm_tracking_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text, 'MILESTONE'::text])))
  CONSTRAINT youtube_content_alarm_tracking_pkey PRIMARY KEY (kind, canonical_content_id)
  INDEX CREATE INDEX idx_ycat_channel_detected ON public.youtube_content_alarm_tracking USING btree (channel_id, detected_at DESC)
  INDEX CREATE INDEX idx_ycat_delivery_status ON public.youtube_content_alarm_tracking USING btree (delivery_status, detected_at DESC)
  INDEX CREATE INDEX idx_ycat_detected_at ON public.youtube_content_alarm_tracking USING btree (detected_at DESC)
  INDEX CREATE INDEX idx_ycat_kind_content ON public.youtube_content_alarm_tracking USING btree (kind, content_id)

TABLE youtube_content_channel_heads
  COLUMN channel_id character varying(50) NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN earliest_complete_effective_at timestamp with time zone
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_content_channel_head_bounds CHECK (((length((channel_id)::text) >= 1) AND (length((channel_id)::text) <= 50)))
  CONSTRAINT chk_youtube_content_channel_head_kind CHECK ((observation_kind = ANY (ARRAY['video_list'::text, 'shorts_list'::text])))
  CONSTRAINT youtube_content_channel_heads_pkey PRIMARY KEY (channel_id, observation_kind)

TABLE youtube_content_evidence_clocks
  COLUMN video_id character varying(20) NOT NULL
  COLUMN first_positive_effective_at timestamp with time zone NOT NULL
  COLUMN last_positive_effective_at timestamp with time zone NOT NULL
  COLUMN last_positive_received_at timestamp with time zone NOT NULL
  COLUMN last_positive_value_sha256 text NOT NULL
  COLUMN last_positive_scope_sha256 text NOT NULL
  COLUMN last_positive_coverage jsonb NOT NULL
  COLUMN last_negative_effective_at timestamp with time zone
  COLUMN last_negative_received_at timestamp with time zone
  COLUMN first_absence_scheduled_for timestamp with time zone
  COLUMN second_absence_scheduled_for timestamp with time zone
  COLUMN last_absence_observation_id bigint
  COLUMN missing_since_effective_at timestamp with time zone
  COLUMN consecutive_absence_slots smallint NOT NULL DEFAULT 0
  COLUMN withdrawn_at timestamp with time zone
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_content_clock_coverage CHECK (((jsonb_typeof(last_positive_coverage) = 'object'::text) AND (octet_length((last_positive_coverage)::text) <= 8192)))
  CONSTRAINT chk_youtube_content_clock_hashes CHECK (((last_positive_value_sha256 ~ '^[0-9a-f]{64}$'::text) AND (last_positive_scope_sha256 ~ '^[0-9a-f]{64}$'::text)))
  CONSTRAINT chk_youtube_content_clock_video_id CHECK (((length((video_id)::text) >= 1) AND (length((video_id)::text) <= 20)))
  CONSTRAINT youtube_content_evidence_clocks_consecutive_absence_slots_check CHECK (((consecutive_absence_slots >= 0) AND (consecutive_absence_slots <= 32767)))
  CONSTRAINT youtube_content_evidence_clock_last_absence_observation_id_fkey FOREIGN KEY (last_absence_observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT youtube_content_evidence_clocks_video_id_fkey FOREIGN KEY (video_id) REFERENCES youtube_videos(video_id) ON DELETE CASCADE
  CONSTRAINT youtube_content_evidence_clocks_pkey PRIMARY KEY (video_id)
  INDEX CREATE INDEX idx_youtube_content_evidence_clocks_last_absence_observation_id ON public.youtube_content_evidence_clocks USING btree (last_absence_observation_id)

TABLE youtube_content_watermarks
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN watermark_type character varying(20) NOT NULL
  COLUMN initialized boolean NOT NULL DEFAULT false
  COLUMN last_content_id character varying(50)
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_content_watermarks_watermark_type_vocab CHECK (((watermark_type)::text = ANY ((ARRAY['VIDEO'::character varying, 'SHORT'::character varying, 'COMMUNITY_POST'::character varying])::text[])))
  CONSTRAINT youtube_content_watermarks_pkey PRIMARY KEY (channel_id, watermark_type)

TABLE youtube_live_reconciliation_heads
  COLUMN video_id text NOT NULL
  COLUMN status text NOT NULL
  COLUMN last_upcoming_positive_at timestamp with time zone
  COLUMN last_upcoming_positive_seen_at timestamp with time zone
  COLUMN last_live_positive_at timestamp with time zone
  COLUMN last_live_positive_seen_at timestamp with time zone
  COLUMN last_end_evidence_at timestamp with time zone
  COLUMN last_complete_absence_at timestamp with time zone
  COLUMN last_absence_scheduled_for timestamp with time zone
  COLUMN consecutive_absence_slots smallint NOT NULL DEFAULT 0
  COLUMN end_candidate_kind text
  COLUMN end_candidate_observation_id bigint
  COLUMN next_end_check_at timestamp with time zone
  COLUMN ended_at timestamp with time zone
  COLUMN end_reason text
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_live_head_candidate_shape CHECK ((((end_candidate_kind IS NULL) AND (end_candidate_observation_id IS NULL) AND (next_end_check_at IS NULL)) OR ((end_candidate_kind IS NOT NULL) AND (end_candidate_observation_id IS NOT NULL) AND (next_end_check_at IS NOT NULL))))
  CONSTRAINT chk_youtube_live_head_video_id CHECK (((length(video_id) >= 1) AND (length(video_id) <= 128)))
  CONSTRAINT youtube_live_reconciliation_hea_consecutive_absence_slots_check CHECK (((consecutive_absence_slots >= 0) AND (consecutive_absence_slots <= 32767)))
  CONSTRAINT youtube_live_reconciliation_heads_end_candidate_kind_check CHECK ((end_candidate_kind = ANY (ARRAY['EXPLICIT_END'::text, 'EXPLICIT_CANCEL'::text, 'SCOPED_ABSENCE'::text])))
  CONSTRAINT youtube_live_reconciliation_heads_end_reason_check CHECK ((end_reason = ANY (ARRAY['EXPLICIT_END'::text, 'CANCELLED_BEFORE_LIVE'::text, 'SCOPED_ABSENCE'::text])))
  CONSTRAINT youtube_live_reconciliation_heads_status_check CHECK ((status = ANY (ARRAY['UPCOMING'::text, 'LIVE'::text, 'ENDED'::text])))
  CONSTRAINT youtube_live_reconciliation_h_end_candidate_observation_id_fkey FOREIGN KEY (end_candidate_observation_id) REFERENCES source_observations(id) ON DELETE RESTRICT
  CONSTRAINT youtube_live_reconciliation_heads_pkey PRIMARY KEY (video_id)
  INDEX CREATE INDEX idx_youtube_live_reconciliation_due ON public.youtube_live_reconciliation_heads USING btree (next_end_check_at, video_id) WHERE (next_end_check_at IS NOT NULL)
  INDEX CREATE INDEX idx_youtube_live_reconciliation_end_candidate ON public.youtube_live_reconciliation_heads USING btree (end_candidate_observation_id) WHERE (end_candidate_observation_id IS NOT NULL)

TABLE youtube_live_sessions
  COLUMN video_id character varying(20) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN status text NOT NULL
  COLUMN title character varying(500)
  COLUMN scheduled_start_time timestamp with time zone
  COLUMN started_at timestamp with time zone
  COLUMN ended_at timestamp with time zone
  COLUMN last_seen_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN live_first_seen_at timestamp with time zone
  COLUMN topic_id text NOT NULL DEFAULT ''::text
  COLUMN thumbnail_url text NOT NULL DEFAULT ''::text
  COLUMN is_premiere boolean
  CONSTRAINT chk_youtube_live_sessions_status_vocab CHECK ((status = ANY (ARRAY[('UPCOMING'::character varying)::text, ('LIVE'::character varying)::text, ('ENDED'::character varying)::text])))
  CONSTRAINT youtube_live_sessions_pkey PRIMARY KEY (video_id)
  INDEX CREATE INDEX idx_yls_channel_last_seen ON public.youtube_live_sessions USING btree (channel_id, last_seen_at DESC)
  INDEX CREATE INDEX idx_yls_ended_channel_sort_video ON public.youtube_live_sessions USING btree (channel_id, COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at) DESC, video_id DESC) WHERE (status = 'ENDED'::text)
  INDEX CREATE INDEX idx_yls_ended_cleanup ON public.youtube_live_sessions USING btree (ended_at, video_id) WHERE ((status = 'ENDED'::text) AND (ended_at IS NOT NULL))
  INDEX CREATE INDEX idx_yls_ended_sort_video ON public.youtube_live_sessions USING btree (COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at) DESC, video_id DESC) WHERE (status = 'ENDED'::text)
  INDEX CREATE INDEX idx_yls_live_first_seen ON public.youtube_live_sessions USING btree (live_first_seen_at, channel_id) WHERE (status = 'LIVE'::text)
  INDEX CREATE INDEX idx_yls_status_last_seen ON public.youtube_live_sessions USING btree (status, last_seen_at DESC)

TABLE youtube_live_viewer_sample_evidence
  COLUMN video_id text NOT NULL
  COLUMN sample_window_start timestamp with time zone NOT NULL
  COLUMN provider text NOT NULL
  COLUMN observation_id bigint
  COLUMN viewer_count bigint
  COLUMN availability text NOT NULL
  COLUMN sample_window_seconds integer NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  CONSTRAINT chk_youtube_viewer_evidence_availability CHECK ((availability = ANY (ARRAY['AVAILABLE'::text, 'HIDDEN'::text, 'UNAVAILABLE'::text])))
  CONSTRAINT chk_youtube_viewer_evidence_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text])))
  CONSTRAINT chk_youtube_viewer_evidence_video_id CHECK (((length(video_id) >= 1) AND (length(video_id) <= 128)))
  CONSTRAINT chk_youtube_viewer_evidence_window CHECK (((sample_window_seconds >= 1) AND (sample_window_seconds <= 86400)))
  CONSTRAINT youtube_live_viewer_sample_evidence_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT youtube_live_viewer_sample_evidence_pkey PRIMARY KEY (video_id, sample_window_start, provider)
  INDEX CREATE INDEX idx_youtube_live_viewer_sample_evidence_observation_id ON public.youtube_live_viewer_sample_evidence USING btree (observation_id)

TABLE youtube_live_viewer_sample_heads
  COLUMN video_id text NOT NULL
  COLUMN last_resolved_window_start timestamp with time zone
  COLUMN last_resolved_count bigint
  COLUMN last_resolved_availability text
  COLUMN prior_resolved_window_start timestamp with time zone
  COLUMN prior_resolved_count bigint
  COLUMN prior_resolved_availability text
  COLUMN unresolved_window_start timestamp with time zone
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_viewer_head_availability CHECK (((last_resolved_availability IS NULL) OR (last_resolved_availability = ANY (ARRAY['AVAILABLE'::text, 'HIDDEN'::text, 'UNAVAILABLE'::text]))))
  CONSTRAINT chk_youtube_viewer_head_video_id CHECK (((length(video_id) >= 1) AND (length(video_id) <= 128)))
  CONSTRAINT youtube_live_viewer_sample_heads_pkey PRIMARY KEY (video_id)

TABLE youtube_live_viewer_samples
  COLUMN video_id character varying(20) NOT NULL
  COLUMN captured_at timestamp with time zone NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN concurrent_viewers integer NOT NULL DEFAULT 0
  CONSTRAINT youtube_live_viewer_samples_pkey PRIMARY KEY (video_id, captured_at)

TABLE youtube_milestone_approaching
  COLUMN id integer NOT NULL DEFAULT nextval('youtube_milestone_approaching_id_seq'::regclass)
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN milestone_value bigint NOT NULL
  COLUMN notified_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN current_subs bigint NOT NULL
  COLUMN chat_notified boolean NOT NULL DEFAULT false
  CONSTRAINT youtube_milestone_approaching_pkey PRIMARY KEY (id)
  CONSTRAINT youtube_milestone_approaching_unique UNIQUE (channel_id, milestone_value)

TABLE youtube_milestones
  COLUMN id integer NOT NULL DEFAULT nextval('youtube_milestones_id_seq'::regclass)
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN member_name text NOT NULL
  COLUMN type character varying(20) NOT NULL
  COLUMN value bigint NOT NULL
  COLUMN achieved_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN notified boolean NOT NULL DEFAULT false
  CONSTRAINT youtube_milestones_pkey PRIMARY KEY (id)
  CONSTRAINT youtube_milestones_unique UNIQUE (channel_id, type, value)

TABLE youtube_notification_delivery
  OPTIONS autovacuum_analyze_scale_factor=0.02,autovacuum_analyze_threshold=50,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=50
  COLUMN id bigint NOT NULL DEFAULT nextval('youtube_notification_delivery_id_seq'::regclass)
  COLUMN outbox_id bigint NOT NULL
  COLUMN room_id character varying(100) NOT NULL
  COLUMN status text NOT NULL DEFAULT 'PENDING'::character varying
  COLUMN attempt_count integer NOT NULL DEFAULT 0
  COLUMN next_attempt_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN locked_at timestamp with time zone
  COLUMN sent_at timestamp with time zone
  COLUMN error text
  CONSTRAINT chk_youtube_notification_delivery_status_vocab CHECK ((status = ANY (ARRAY[('PENDING'::character varying)::text, ('SENDING'::character varying)::text, ('SENT'::character varying)::text, ('FAILED'::character varying)::text, ('QUARANTINED'::character varying)::text])))
  CONSTRAINT youtube_notification_delivery_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES youtube_notification_outbox(id) ON DELETE CASCADE
  CONSTRAINT youtube_notification_delivery_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX idx_ynd_outbox_room ON public.youtube_notification_delivery USING btree (outbox_id, room_id)
  INDEX CREATE INDEX idx_ynd_pending_due_created_id ON public.youtube_notification_delivery USING btree (next_attempt_at, created_at, id) WHERE (status = 'PENDING'::text)
  INDEX CREATE INDEX idx_ynd_sending_stale ON public.youtube_notification_delivery USING btree (locked_at, id) WHERE (status = 'SENDING'::text)

TABLE youtube_notification_delivery_telemetry
  OPTIONS autovacuum_analyze_scale_factor=0.02,autovacuum_analyze_threshold=50,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=50
  COLUMN id bigint NOT NULL DEFAULT nextval('youtube_notification_delivery_telemetry_id_seq'::regclass)
  COLUMN delivery_id bigint NOT NULL
  COLUMN attempt_ordinal integer NOT NULL
  COLUMN outbox_id bigint NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN content_id character varying(50) NOT NULL
  COLUMN room_id character varying(100) NOT NULL
  COLUMN alarm_type text NOT NULL
  COLUMN dedupe_key text NOT NULL
  COLUMN delivery_mode character varying(20) NOT NULL
  COLUMN send_result character varying(20) NOT NULL
  COLUMN failure_reason character varying(100)
  COLUMN event_at timestamp with time zone NOT NULL
  COLUMN next_attempt_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN locked_at timestamp with time zone
  COLUMN logged_at timestamp with time zone
  COLUMN error text
  COLUMN delivery_path character varying(100) NOT NULL DEFAULT 'youtube_outbox_dispatcher'::character varying
  COLUMN post_id character varying(50) NOT NULL
  COLUMN attempt_started_at timestamp with time zone
  COLUMN attempt_finished_at timestamp with time zone
  COLUMN actual_published_at timestamp with time zone
  COLUMN detected_at timestamp with time zone
  COLUMN alarm_sent_at timestamp with time zone
  COLUMN alarm_latency_millis bigint
  CONSTRAINT chk_youtube_notification_delivery_telemetry_alarm_type_vocab CHECK ((alarm_type = ANY (ARRAY[('LIVE'::character varying)::text, ('COMMUNITY'::character varying)::text, ('SHORTS'::character varying)::text, ('BIRTHDAY'::character varying)::text, ('ANNIVERSARY'::character varying)::text])))
  CONSTRAINT youtube_notification_delivery_telemetry_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX idx_ydt_delivery_attempt ON public.youtube_notification_delivery_telemetry USING btree (delivery_id, attempt_ordinal)
  INDEX CREATE INDEX idx_ydt_logged_event_retention ON public.youtube_notification_delivery_telemetry USING btree (event_at, id) WHERE (logged_at IS NOT NULL)
  INDEX CREATE INDEX idx_ydt_outbox ON public.youtube_notification_delivery_telemetry USING btree (outbox_id)
  INDEX CREATE INDEX idx_ydt_pending_next ON public.youtube_notification_delivery_telemetry USING btree (next_attempt_at, event_at) WHERE (logged_at IS NULL)

TABLE youtube_notification_outbox
  OPTIONS autovacuum_analyze_scale_factor=0.02,autovacuum_analyze_threshold=50,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=50
  COLUMN id bigint NOT NULL DEFAULT nextval('youtube_notification_outbox_id_seq'::regclass)
  COLUMN kind text NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN content_id character varying(50) NOT NULL
  COLUMN payload jsonb NOT NULL
  COLUMN status text NOT NULL DEFAULT 'PENDING'::character varying
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN locked_at timestamp with time zone
  COLUMN sent_at timestamp with time zone
  COLUMN error text
  COLUMN attempt_count integer NOT NULL DEFAULT 0
  COLUMN next_attempt_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_notification_outbox_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text, 'MILESTONE'::text])))
  CONSTRAINT chk_youtube_notification_outbox_status_vocab CHECK ((status = ANY (ARRAY[('PENDING'::character varying)::text, ('SENT'::character varying)::text, ('FAILED'::character varying)::text])))
  CONSTRAINT youtube_notification_outbox_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX idx_yno_kind_content ON public.youtube_notification_outbox USING btree (kind, content_id)
  INDEX CREATE INDEX idx_yno_pending_due_created_id ON public.youtube_notification_outbox USING btree (next_attempt_at, created_at, id) WHERE (status = 'PENDING'::text)
  INDEX CREATE INDEX idx_yno_status_created ON public.youtube_notification_outbox USING btree (status, created_at)

TABLE youtube_schedule_items
  COLUMN group_key text NOT NULL
  COLUMN provider text NOT NULL
  COLUMN external_id text NOT NULL
  COLUMN video_id text NOT NULL DEFAULT ''::text
  COLUMN channel_id text NOT NULL DEFAULT ''::text
  COLUMN title text NOT NULL
  COLUMN scheduled_at timestamp with time zone NOT NULL
  COLUMN ended_at timestamp with time zone
  COLUMN is_live boolean NOT NULL DEFAULT false
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_schedule_item_bounds CHECK ((((length(group_key) >= 1) AND (length(group_key) <= 256)) AND ((length(external_id) >= 1) AND (length(external_id) <= 256)) AND (length(video_id) <= 128) AND (length(channel_id) <= 256) AND ((length(title) >= 1) AND (length(title) <= 4096))))
  CONSTRAINT chk_youtube_schedule_item_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text])))
  CONSTRAINT youtube_schedule_items_pkey PRIMARY KEY (group_key, provider, external_id)

TABLE youtube_stats_changes
  COLUMN id integer NOT NULL DEFAULT nextval('youtube_stats_changes_id_seq'::regclass)
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN member_name text
  COLUMN subscriber_change bigint NOT NULL DEFAULT 0
  COLUMN video_change bigint NOT NULL DEFAULT 0
  COLUMN view_change bigint NOT NULL DEFAULT 0
  COLUMN previous_subs bigint
  COLUMN current_subs bigint
  COLUMN previous_videos bigint
  COLUMN current_videos bigint
  COLUMN detected_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN notified boolean NOT NULL DEFAULT false
  CONSTRAINT youtube_stats_changes_pkey PRIMARY KEY (id)

TABLE youtube_stats_history
  COLUMN time timestamp with time zone NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN member_name text
  COLUMN subscribers bigint
  COLUMN videos bigint
  COLUMN views bigint
  CONSTRAINT youtube_stats_history_pkey PRIMARY KEY ("time", channel_id)
  INDEX CREATE INDEX idx_youtube_stats_history_channel_time ON public.youtube_stats_history USING btree (channel_id, "time" DESC)

TABLE youtube_stream_stats
  COLUMN video_id character varying(20) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN started_at timestamp with time zone
  COLUMN ended_at timestamp with time zone
  COLUMN max_concurrent_viewers integer DEFAULT 0
  COLUMN avg_concurrent_viewers integer DEFAULT 0
  COLUMN sample_count integer NOT NULL DEFAULT 0
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT youtube_stream_stats_pkey PRIMARY KEY (video_id)

TABLE youtube_videos
  COLUMN video_id character varying(20) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN title character varying(500) NOT NULL
  COLUMN thumbnail jsonb
  COLUMN duration character varying(20)
  COLUMN published_text character varying(100)
  COLUMN published_at timestamp with time zone
  COLUMN is_short boolean NOT NULL DEFAULT false
  COLUMN is_live_replay boolean NOT NULL DEFAULT false
  COLUMN view_count bigint DEFAULT 0
  COLUMN first_seen_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN last_seen_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT youtube_videos_pkey PRIMARY KEY (video_id)
  INDEX CREATE INDEX idx_yv_channel_first_seen ON public.youtube_videos USING btree (channel_id, first_seen_at DESC)
  INDEX CREATE INDEX idx_yv_channel_is_short ON public.youtube_videos USING btree (channel_id, is_short)

SEQUENCE acl_rooms_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY acl_rooms.id

SEQUENCE acl_settings_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY acl_settings.id

SEQUENCE alarm_dispatch_admin_actions_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY alarm_dispatch_admin_actions.id

SEQUENCE alarm_dispatch_deliveries_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY alarm_dispatch_deliveries.id

SEQUENCE alarm_dispatch_event_collisions_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY alarm_dispatch_event_collisions.id

SEQUENCE alarm_dispatch_events_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY alarm_dispatch_events.id

SEQUENCE alarm_dispatch_send_units_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY alarm_dispatch_send_units.id

SEQUENCE alarms_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY alarms.id

SEQUENCE bot_command_executions_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY bot_command_executions.id

SEQUENCE bot_reply_outbox_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY bot_reply_outbox.id

SEQUENCE bot_reply_outbox_replay_audit_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY bot_reply_outbox_replay_audit.id

SEQUENCE bot_webhook_inbox_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY bot_webhook_inbox.id

SEQUENCE major_event_subscriptions_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY major_event_subscriptions.id

SEQUENCE major_events_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY major_events.id

SEQUENCE member_news_subscriptions_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY member_news_subscriptions.id

SEQUENCE members_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY members.id

SEQUENCE message_strings_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY message_strings.id

SEQUENCE notification_delivery_outbox_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY notification_delivery_outbox.id

SEQUENCE notification_template_revisions_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY notification_template_revisions.id

SEQUENCE notification_templates_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY notification_templates.id

SEQUENCE source_observation_applications_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_observation_applications.id

SEQUENCE source_observation_collisions_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_observation_collisions.id

SEQUENCE source_observation_replay_requests_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_observation_replay_requests.id

SEQUENCE source_observations_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_observations.id

SEQUENCE source_reconciliation_conflicts_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_reconciliation_conflicts.id

SEQUENCE youtube_collection_projection_generations_generation_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY youtube_collection_projection_generations.generation

SEQUENCE youtube_milestone_approaching_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY youtube_milestone_approaching.id

SEQUENCE youtube_milestones_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY youtube_milestones.id

SEQUENCE youtube_notification_delivery_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY youtube_notification_delivery.id

SEQUENCE youtube_notification_delivery_telemetry_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY youtube_notification_delivery_telemetry.id

SEQUENCE youtube_notification_outbox_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY youtube_notification_outbox.id

SEQUENCE youtube_stats_changes_id_seq AS integer START 1 INCREMENT 1 MIN 1 MAX 2147483647 CACHE 1 CYCLE false OWNED BY youtube_stats_changes.id

FUNCTION append_bot_reply_outbox_replay_claim_audit() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nDECLARE\n    granted_actor TEXT;\n    granted_reason TEXT;\nBEGIN\n    IF NEW.status = 'submitting'\n        AND OLD.status <> 'submitting'\n        AND NEW.operator_replay_grants > 0\n    THEN\n        SELECT actor, reason\n        INTO granted_actor, granted_reason\n        FROM public.bot_reply_outbox_replay_audit\n        WHERE outbox_id = NEW.id\n          AND grant_number = NEW.operator_replay_grants\n          AND event_type = 'granted';\n\n        IF NOT FOUND THEN\n            RAISE EXCEPTION 'manual replay grant audit is missing for outbox %, grant %',\n                NEW.id, NEW.operator_replay_grants\n                USING ERRCODE = '23514';\n        END IF;\n\n        INSERT INTO public.bot_reply_outbox_replay_audit (\n            outbox_id, grant_number, event_type, actor, reason\n        ) VALUES (\n            NEW.id, NEW.operator_replay_grants, 'replayed', granted_actor, granted_reason\n        )\n        ON CONFLICT (outbox_id, grant_number, event_type) DO NOTHING;\n    END IF;\n\n    RETURN NEW;\nEND\n"

FUNCTION delete_retired_youtube_collection_job_leases(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_job_key text) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    WITH candidate AS (\n        SELECT lease.job_key\n        FROM public.youtube_collection_job_leases AS lease\n        JOIN public.youtube_collection_projection_generations AS generation\n          ON generation.generation = lease.projection_generation\n        WHERE generation.status = 'RETIRED'\n          AND generation.valid_until < requested_cutoff\n          AND (\n              lease.slot_state <> 'ACTIVE'\n              OR lease.lease_expires_at < clock_timestamp()\n          )\n        ORDER BY generation.generation, lease.job_key\n        LIMIT CASE\n            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n            ELSE 0\n        END\n        FOR UPDATE OF lease SKIP LOCKED\n    )\n    DELETE FROM public.youtube_collection_job_leases AS lease\n    USING candidate\n    WHERE lease.job_key = candidate.job_key\n    RETURNING lease.job_key\n"

FUNCTION delete_source_observation_retention_batch(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer) RETURNS TABLE(deleted_id bigint) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    WITH candidates AS (\n        SELECT candidate.id\n        FROM public.source_observations AS candidate\n        JOIN pg_catalog.generate_subscripts(requested_kinds, 1) AS policy(position)\n          ON requested_kinds[policy.position] = candidate.observation_kind\n        LEFT JOIN public.source_observation_queue AS queue\n          ON queue.observation_id = candidate.id\n        WHERE pg_catalog.cardinality(requested_kinds) BETWEEN 1 AND 16\n          AND pg_catalog.cardinality(requested_kinds) = pg_catalog.cardinality(requested_cutoffs)\n          AND candidate.received_at < requested_cutoffs[policy.position]\n          AND queue.observation_id IS NULL\n          AND NOT EXISTS (\n              SELECT 1\n              FROM public.source_observation_replay_requests AS replay\n              WHERE replay.observation_id = candidate.id\n                AND replay.status = 'PENDING'\n          )\n          AND NOT EXISTS (\n              SELECT 1\n              FROM public.youtube_live_reconciliation_heads AS head\n              WHERE head.end_candidate_observation_id = candidate.id\n          )\n        ORDER BY candidate.received_at, candidate.id\n        LIMIT CASE\n            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n            ELSE 0\n        END\n        FOR UPDATE OF candidate SKIP LOCKED\n    )\n    DELETE FROM public.source_observations AS observation\n    USING candidates\n    WHERE observation.id = candidates.id\n      AND NOT EXISTS (\n          SELECT 1\n          FROM public.source_observation_queue AS live_queue\n          WHERE live_queue.observation_id = observation.id\n      )\n      AND NOT EXISTS (\n          SELECT 1\n          FROM public.source_observation_replay_requests AS live_replay\n          WHERE live_replay.observation_id = observation.id\n            AND live_replay.status = 'PENDING'\n      )\n      AND NOT EXISTS (\n          SELECT 1\n          FROM public.youtube_live_reconciliation_heads AS live_head\n          WHERE live_head.end_candidate_observation_id = observation.id\n      )\n    RETURNING observation.id\n"

FUNCTION grant_bot_reply_outbox_manual_replay(requested_outbox_id bigint, operator_actor text, operator_reason text) RETURNS text LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nDECLARE\n    granted_at TIMESTAMPTZ := clock_timestamp();\n    normalized_actor TEXT := btrim(operator_actor);\n    normalized_reason TEXT := btrim(operator_reason);\n    target_id BIGINT;\n    target_status TEXT;\n    target_created_at TIMESTAMPTZ;\n    target_replay_grants INTEGER;\n    next_grant_number INTEGER;\nBEGIN\n    SELECT id, status, created_at, operator_replay_grants\n    INTO target_id, target_status, target_created_at, target_replay_grants\n    FROM public.bot_reply_outbox\n    WHERE id = requested_outbox_id\n    FOR UPDATE;\n\n    IF NOT FOUND THEN\n        RETURN 'not_found';\n    END IF;\n    IF target_status <> 'manual_review' THEN\n        RETURN 'not_manual_review';\n    END IF;\n    IF granted_at >= target_created_at + interval '144 hours' THEN\n        RETURN 'cutoff_expired';\n    END IF;\n    IF normalized_actor !~ '^[A-Za-z0-9._:@-]{1,64}$'\n        OR octet_length(normalized_reason) NOT BETWEEN 1 AND 256\n        OR normalized_reason ~ '[[:cntrl:]]'\n    THEN\n        RETURN 'invalid_operator_metadata';\n    END IF;\n\n    next_grant_number := target_replay_grants + 1;\n    INSERT INTO public.bot_reply_outbox_replay_audit (\n        outbox_id, grant_number, event_type, actor, reason, recorded_at\n    ) VALUES (\n        target_id, next_grant_number, 'granted', normalized_actor, normalized_reason, granted_at\n    );\n\n    UPDATE public.bot_reply_outbox\n    SET status = 'pending',\n        claim_token = NULL,\n        lease_until = NULL,\n        last_error = '',\n        operator_replay_grants = next_grant_number,\n        available_at = granted_at,\n        updated_at = granted_at\n    WHERE id = target_id;\n\n    RETURN 'replayed';\nEND\n"

FUNCTION lock_observation_contract(requested_provider text, requested_observation_kind text) RETURNS TABLE(current_schema_version smallint, current_generation bigint) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    SELECT contract.current_schema_version,\n           contract.current_generation\n    FROM public.observation_contract_generations AS contract\n    WHERE contract.provider = requested_provider\n      AND contract.observation_kind = requested_observation_kind\n    FOR SHARE OF contract\n"

FUNCTION lock_source_observation(requested_observation_id bigint) RETURNS TABLE(provider text, observation_kind text, subject_key text, observation_key text, schema_version smallint, contract_generation bigint, evidence_sha256 text) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    SELECT observation.provider,\n           observation.observation_kind,\n           observation.subject_key,\n           observation.observation_key,\n           observation.schema_version,\n           observation.contract_generation,\n           observation.evidence_sha256\n    FROM public.source_observations AS observation\n    WHERE observation.id = requested_observation_id\n    FOR SHARE OF observation\n"

FUNCTION lock_source_observation_identity(requested_provider text, requested_observation_kind text, requested_subject_key text, requested_observation_key text, requested_schema_version smallint, requested_contract_generation bigint) RETURNS TABLE(id bigint, evidence_sha256 text) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    SELECT observation.id,\n           observation.evidence_sha256\n    FROM public.source_observations AS observation\n    WHERE observation.provider = requested_provider\n      AND observation.observation_kind = requested_observation_kind\n      AND observation.subject_key = requested_subject_key\n      AND observation.observation_key = requested_observation_key\n      AND observation.schema_version = requested_schema_version\n      AND observation.contract_generation = requested_contract_generation\n    FOR SHARE OF observation\n"

FUNCTION lock_youtube_collection_projection(requested_generation bigint) RETURNS TABLE(generation bigint) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    SELECT projection.generation\n    FROM public.youtube_collection_projection_generations AS projection\n    WHERE projection.generation = requested_generation\n      AND projection.status = 'CURRENT'\n      AND projection.valid_until > clock_timestamp()\n    FOR SHARE OF projection\n"

FUNCTION reject_bot_reply_outbox_replay_audit_mutation() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nBEGIN\n    IF TG_OP = 'DELETE'\n        AND NOT EXISTS (\n            SELECT 1\n            FROM public.bot_reply_outbox\n            WHERE id = OLD.outbox_id\n        )\n    THEN\n        RETURN OLD;\n    END IF;\n\n    RAISE EXCEPTION 'bot_reply_outbox_replay_audit events are immutable'\n        USING ERRCODE = '55000';\nEND\n"

FUNCTION scrub_bot_command_execution_terminal_summary() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\nBEGIN\n    NEW.result_summary := NEW.status;\n    RETURN NEW;\nEND\n"

FUNCTION scrub_bot_webhook_inbox_terminal_payload() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\nBEGIN\n    NEW.payload := '{}'::jsonb;\n    RETURN NEW;\nEND\n"
