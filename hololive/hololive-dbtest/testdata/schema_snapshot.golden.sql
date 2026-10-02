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

TABLE alarm_dispatch_closeout_receipts
  COLUMN receipt_id uuid NOT NULL
  COLUMN send_unit_id bigint NOT NULL
  COLUMN addressed_delivery_id bigint NOT NULL
  COLUMN target_ids bigint[] NOT NULL
  COLUMN target_revisions jsonb NOT NULL
  COLUMN status_metadata jsonb NOT NULL
  COLUMN original_sha256 text NOT NULL
  COLUMN operator_id text NOT NULL
  COLUMN reason text NOT NULL
  COLUMN disposition text NOT NULL DEFAULT 'closed_without_replay'::text
  COLUMN recorded_at timestamp with time zone NOT NULL DEFAULT clock_timestamp()
  CONSTRAINT alarm_dispatch_closeout_receipts_digest_check CHECK ((original_sha256 ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT alarm_dispatch_closeout_receipts_disposition_check CHECK ((disposition = 'closed_without_replay'::text))
  CONSTRAINT alarm_dispatch_closeout_receipts_operator_check CHECK ((((length(operator_id) >= 1) AND (length(operator_id) <= 128)) AND (operator_id = btrim(operator_id)) AND (operator_id !~ '[[:cntrl:]]'::text)))
  CONSTRAINT alarm_dispatch_closeout_receipts_reason_check CHECK ((((length(reason) >= 1) AND (length(reason) <= 1024)) AND (reason = btrim(reason)) AND (reason !~ '[[:cntrl:]]'::text)))
  CONSTRAINT alarm_dispatch_closeout_receipts_targets_check CHECK ((((cardinality(target_ids) >= 1) AND (cardinality(target_ids) <= 100)) AND (jsonb_array_length(target_revisions) = cardinality(target_ids)) AND (jsonb_array_length(status_metadata) = cardinality(target_ids))))
  CONSTRAINT alarm_dispatch_closeout_receipts_pkey PRIMARY KEY (receipt_id)
  CONSTRAINT alarm_dispatch_closeout_receipts_send_unit_id_key UNIQUE (send_unit_id)
  TRIGGER CREATE TRIGGER alarm_dispatch_closeout_receipts_immutable BEFORE DELETE OR UPDATE ON alarm_dispatch_closeout_receipts FOR EACH ROW EXECUTE FUNCTION reject_alarm_dispatch_closeout_receipt_change()
  TRIGGER CREATE TRIGGER alarm_dispatch_closeout_receipts_no_truncate BEFORE TRUNCATE ON alarm_dispatch_closeout_receipts FOR EACH STATEMENT EXECUTE FUNCTION reject_alarm_dispatch_closeout_receipt_change()

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
  CONSTRAINT alarm_dispatch_deliveries_active_send_unit_check CHECK (((send_unit_id IS NOT NULL) OR (status <> ALL (ARRAY['pending'::text, 'retry'::text, 'leased'::text, 'sending'::text]))))
  CONSTRAINT alarm_dispatch_deliveries_attempt_check CHECK ((attempt_count >= 0))
  CONSTRAINT alarm_dispatch_deliveries_dedupe_key_check CHECK (((length(dedupe_key) > 0) AND (length(dedupe_key) <= 768)))
  CONSTRAINT alarm_dispatch_deliveries_dispatch_group_key_check CHECK (((dispatch_group_key IS NULL) OR ((length(dispatch_group_key) > 0) AND (length(dispatch_group_key) <= 768))))
  CONSTRAINT alarm_dispatch_deliveries_room_id_check CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100)))
  CONSTRAINT alarm_dispatch_deliveries_send_unit_pair_check CHECK (((dispatch_group_key IS NULL) = (send_unit_id IS NULL)))
  CONSTRAINT alarm_dispatch_deliveries_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'retry'::text, 'leased'::text, 'sending'::text, 'sent'::text, 'dlq'::text, 'quarantined'::text, 'cancelled'::text])))
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
  COLUMN request_body text
  COLUMN request_route text
  COLUMN request_body_hash text
  COLUMN request_delivery_ids bigint[]
  COLUMN base_client_request_id text
  COLUMN request_generation integer NOT NULL DEFAULT 0
  CONSTRAINT alarm_dispatch_send_units_client_request_id_check CHECK ((client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'::text))
  CONSTRAINT alarm_dispatch_send_units_group_key_check CHECK (((length(dispatch_group_key) > 0) AND (length(dispatch_group_key) <= 768)))
  CONSTRAINT alarm_dispatch_send_units_room_id_check CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100)))
  CONSTRAINT alarm_dispatch_send_units_unit_key_check CHECK ((unit_key ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT chk_alarm_send_request_shape CHECK ((((request_generation >= 0) AND (request_generation <= 2)) AND (((request_body IS NULL) AND (request_route IS NULL) AND (request_body_hash IS NULL) AND (request_delivery_ids IS NULL) AND (base_client_request_id IS NULL) AND (request_generation = 0)) OR ((request_body IS NOT NULL) AND (request_route IS NOT NULL) AND (request_route = ANY (ARRAY['text'::text, 'markdown'::text])) AND (request_body_hash IS NOT NULL) AND (request_body_hash ~ '^[0-9a-f]{64}$'::text) AND (request_delivery_ids IS NOT NULL) AND ((cardinality(request_delivery_ids) >= 1) AND (cardinality(request_delivery_ids) <= 10)) AND (base_client_request_id IS NOT NULL) AND (base_client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'::text)))))
  CONSTRAINT alarm_dispatch_send_units_pkey PRIMARY KEY (id)
  CONSTRAINT alarm_dispatch_send_units_client_request_id_key UNIQUE (client_request_id)
  CONSTRAINT alarm_dispatch_send_units_unit_key_key UNIQUE (unit_key)

TABLE alarm_room_display_names
  COLUMN room_id character varying(100) NOT NULL
  COLUMN display_name character varying(255) NOT NULL
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_alarm_room_display_names_display_name_nonblank CHECK ((btrim((display_name)::text) <> ''::text))
  CONSTRAINT alarm_room_display_names_pkey PRIMARY KEY (room_id)

TABLE alarm_upcoming_candidates
  COLUMN dedupe_key text NOT NULL
  COLUMN event_key text NOT NULL
  COLUMN payload_hash text NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN stream_id text NOT NULL
  COLUMN room_id character varying(100) NOT NULL
  COLUMN scheduled_at timestamp with time zone NOT NULL
  COLUMN notification jsonb NOT NULL
  COLUMN selected_at timestamp with time zone NOT NULL
  COLUMN checked_at timestamp with time zone NOT NULL
  COLUMN terminal_at timestamp with time zone
  COLUMN outcome text NOT NULL DEFAULT 'pending'::text
  CONSTRAINT chk_alarm_upcoming_candidates_outcome_vocab CHECK ((outcome = ANY (ARRAY['pending'::text, 'accepted'::text, 'rejected_collision'::text, 'rejected_terminal'::text, 'expired'::text, 'schedule_changed'::text, 'stream_ended'::text, 'subscription_removed'::text])))
  CONSTRAINT chk_alarm_upcoming_candidates_terminal CHECK (((outcome = 'pending'::text) = (terminal_at IS NULL)))
  CONSTRAINT alarm_upcoming_candidates_pkey PRIMARY KEY (dedupe_key)
  INDEX CREATE INDEX idx_alarm_upcoming_candidates_pending ON public.alarm_upcoming_candidates USING btree (checked_at, dedupe_key) WHERE (outcome = 'pending'::text)
  INDEX CREATE INDEX idx_alarm_upcoming_candidates_terminal ON public.alarm_upcoming_candidates USING btree (terminal_at) WHERE (terminal_at IS NOT NULL)

TABLE alarm_upcoming_checkpoints
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN evaluated_at timestamp with time zone NOT NULL
  CONSTRAINT alarm_upcoming_checkpoints_pkey PRIMARY KEY (channel_id)

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
  COLUMN host_id text NOT NULL DEFAULT ''::text
  COLUMN room_name_updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_alarms_host_id_vocab CHECK (((host_id = ''::text) OR (((channel_id)::text = 'UC3OH5FKQ3qtl4uRme_vZTgA'::text) AND (host_id = ANY (ARRAY['kiyosumi-lyra'::text, 'reimei-mira'::text, 'yoinagi-neon'::text])))))
  CONSTRAINT alarms_pkey PRIMARY KEY (id)
  INDEX CREATE INDEX idx_alarms_alarm_types_gin ON public.alarms USING gin (alarm_types)
  INDEX CREATE INDEX idx_alarms_channel_created ON public.alarms USING btree (channel_id, created_at)
  INDEX CREATE INDEX idx_alarms_channel_member_latest ON public.alarms USING btree (channel_id, created_at DESC) WHERE ((member_name IS NOT NULL) AND (member_name <> ''::text))
  INDEX CREATE UNIQUE INDEX idx_alarms_room_channel_host ON public.alarms USING btree (room_id, channel_id, host_id)
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
  COLUMN session_generation bigint NOT NULL DEFAULT 0
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
  CONSTRAINT chk_bot_reply_outbox_state_shape CHECK ((((status <> ALL (ARRAY['submitting'::text, 'accepted'::text])) OR ((claim_token IS NOT NULL) AND (lease_until IS NOT NULL) AND (first_attempt_at IS NOT NULL))) AND ((status <> 'accepted'::text) OR (length(iris_request_id) > 0)) AND ((status = ANY (ARRAY['handoff_completed'::text, 'dead'::text, 'permanent_conflict'::text, 'discarded'::text])) OR (payload IS NOT NULL))))
  CONSTRAINT chk_bot_reply_outbox_status_vocab CHECK ((status = ANY (ARRAY['pending'::text, 'submitting'::text, 'accepted'::text, 'handoff_completed'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text, 'dead'::text, 'permanent_conflict'::text, 'manual_review'::text, 'discarded'::text])))
  CONSTRAINT bot_reply_outbox_pkey PRIMARY KEY (id)
  CONSTRAINT bot_reply_outbox_client_request_id_key UNIQUE (client_request_id)
  CONSTRAINT bot_reply_outbox_message_id_phase_ordinal_key UNIQUE (message_id, phase, ordinal)
  INDEX CREATE INDEX idx_bot_reply_outbox_discarded_updated ON public.bot_reply_outbox USING btree (updated_at, id) WHERE (status = 'discarded'::text)
  INDEX CREATE INDEX idx_bot_reply_outbox_due_available ON public.bot_reply_outbox USING btree (available_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text]))
  INDEX CREATE INDEX idx_bot_reply_outbox_lease_expiry ON public.bot_reply_outbox USING btree (lease_until, id) WHERE (status = ANY (ARRAY['submitting'::text, 'accepted'::text]))
  INDEX CREATE INDEX idx_bot_reply_outbox_manual_review_updated ON public.bot_reply_outbox USING btree (updated_at, id) WHERE (status = 'manual_review'::text)
  INDEX CREATE INDEX idx_bot_reply_outbox_message ON public.bot_reply_outbox USING btree (message_id, ordinal)
  INDEX CREATE INDEX idx_bot_reply_outbox_room_active ON public.bot_reply_outbox USING btree (room_id, id) WHERE (status = ANY (ARRAY['pending'::text, 'submitting'::text, 'accepted'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text]))
  INDEX CREATE INDEX idx_bot_reply_outbox_terminal_updated ON public.bot_reply_outbox USING btree (updated_at, id) WHERE (status = ANY (ARRAY['handoff_completed'::text, 'dead'::text, 'permanent_conflict'::text]))
  TRIGGER CREATE TRIGGER bot_reply_outbox_discard_audit_required BEFORE INSERT OR UPDATE ON bot_reply_outbox FOR EACH ROW EXECUTE FUNCTION enforce_bot_reply_outbox_discard_audit()
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

TABLE bot_reply_outbox_resolution_audit
  COLUMN id bigint NOT NULL DEFAULT nextval('bot_reply_outbox_resolution_audit_id_seq'::regclass)
  COLUMN outbox_id bigint NOT NULL
  COLUMN decision text NOT NULL
  COLUMN observed_iris_state text NOT NULL
  COLUMN actor text NOT NULL
  COLUMN reason text NOT NULL
  COLUMN recorded_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_bot_reply_outbox_resolution_audit_actor CHECK ((actor ~ '^[A-Za-z0-9._:@-]{1,64}$'::text))
  CONSTRAINT chk_bot_reply_outbox_resolution_audit_decision CHECK ((decision = 'discarded_without_replay'::text))
  CONSTRAINT chk_bot_reply_outbox_resolution_audit_iris_state CHECK ((observed_iris_state = ANY (ARRAY['queued'::text, 'preparing'::text, 'prepared'::text, 'sending'::text, 'handoff_completed'::text, 'failed'::text, 'outcome_unknown'::text, 'not_found'::text])))
  CONSTRAINT chk_bot_reply_outbox_resolution_audit_reason CHECK ((((octet_length(reason) >= 1) AND (octet_length(reason) <= 256)) AND (reason !~ '[[:cntrl:]]'::text)))
  CONSTRAINT bot_reply_outbox_resolution_audit_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES bot_reply_outbox(id) ON DELETE CASCADE
  CONSTRAINT bot_reply_outbox_resolution_audit_pkey PRIMARY KEY (id)
  CONSTRAINT bot_reply_outbox_resolution_audit_outbox_id_key UNIQUE (outbox_id)
  TRIGGER CREATE TRIGGER bot_reply_outbox_resolution_audit_immutable BEFORE DELETE OR UPDATE ON bot_reply_outbox_resolution_audit FOR EACH ROW EXECUTE FUNCTION reject_bot_reply_outbox_resolution_audit_mutation()

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
  COLUMN units text[] NOT NULL DEFAULT '{}'::text[]
  COLUMN official_link text
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
  COLUMN row_version bigint NOT NULL DEFAULT 1
  CONSTRAINT notification_templates_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX ux_notification_templates_channel ON public.notification_templates USING btree (template_key, channel_id) WHERE (channel_id IS NOT NULL)
  INDEX CREATE UNIQUE INDEX ux_notification_templates_default ON public.notification_templates USING btree (template_key) WHERE (channel_id IS NULL)
  TRIGGER CREATE TRIGGER notification_template_row_version_trigger BEFORE UPDATE ON notification_templates FOR EACH ROW EXECUTE FUNCTION notification_template_row_version()

TABLE observation_contract_generations
  COLUMN provider text NOT NULL
  COLUMN observation_kind text NOT NULL
  COLUMN current_schema_version smallint NOT NULL
  COLUMN current_generation bigint NOT NULL
  COLUMN updated_by text NOT NULL
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_observation_contract_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'viewer_sample'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text, 'channel_live_check'::text, 'video_live_check'::text])))
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
  INDEX CREATE INDEX idx_source_collection_checkpoints_updated_identity ON public.source_collection_checkpoints USING btree (updated_at, provider, observation_kind, subject_key, scope_sha256)

TABLE source_observation_applications
  OPTIONS autovacuum_vacuum_scale_factor=0.05,autovacuum_vacuum_threshold=500
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
  INDEX CREATE INDEX idx_source_application_live_origin ON public.source_observation_applications USING btree (entity_key) WHERE ((entity_kind = 'youtube_live_session'::text) AND (decision = ANY (ARRAY['APPLIED'::text, 'ENDED'::text])) AND (observation_kind = ANY (ARRAY['live_snapshot'::text, 'video_live_check'::text])))
  INDEX CREATE INDEX idx_source_observation_applications_orphaned_kind_applied_id ON public.source_observation_applications USING btree (observation_kind, applied_at, id) WHERE (observation_id IS NULL)
  INDEX CREATE UNIQUE INDEX uq_source_observation_application_active ON public.source_observation_applications USING btree (observation_id, entity_kind, entity_key) WHERE (observation_id IS NOT NULL)

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
  CONSTRAINT chk_source_observation_consumer_offset_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'viewer_sample'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text, 'channel_live_check'::text, 'video_live_check'::text])))
  CONSTRAINT source_observation_consumer_offsets_last_processed_id_check CHECK ((last_processed_id >= 0))
  CONSTRAINT source_observation_consumer_offsets_pkey PRIMARY KEY (consumer_name, observation_kind)

TABLE source_observation_payload_gc_state
  COLUMN singleton boolean NOT NULL DEFAULT true
  COLUMN cursor_id bigint NOT NULL DEFAULT 0
  CONSTRAINT source_observation_payload_gc_state_cursor_id_check CHECK ((cursor_id >= 0))
  CONSTRAINT source_observation_payload_gc_state_singleton_check CHECK (singleton)
  CONSTRAINT source_observation_payload_gc_state_pkey PRIMARY KEY (singleton)

TABLE source_observation_payloads
  COLUMN id bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY
  COLUMN observation_kind text NOT NULL
  COLUMN schema_version smallint NOT NULL
  COLUMN canonical_profile text NOT NULL
  COLUMN payload_sha256 bytea NOT NULL
  COLUMN payload jsonb NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT source_observation_payloads_canonical_profile_check CHECK ((canonical_profile = 'source-observation-canonical-json-v1'::text))
  CONSTRAINT source_observation_payloads_payload_check CHECK (((jsonb_typeof(payload) = 'object'::text) AND (octet_length((payload)::text) <= 1048576)))
  CONSTRAINT source_observation_payloads_payload_sha256_check CHECK ((octet_length(payload_sha256) = 32))
  CONSTRAINT source_observation_payloads_schema_version_check CHECK ((schema_version > 0))
  CONSTRAINT source_observation_payloads_pkey PRIMARY KEY (id)
  CONSTRAINT uq_source_observation_payload_digest UNIQUE (observation_kind, schema_version, canonical_profile, payload_sha256)

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

TABLE source_observation_replay_epoch
  COLUMN singleton boolean NOT NULL DEFAULT true
  COLUMN cutoff_received_at timestamp with time zone NOT NULL
  COLUMN activated_by text NOT NULL
  COLUMN reason text NOT NULL
  CONSTRAINT chk_source_observation_replay_epoch_attribution CHECK ((((length(btrim(activated_by)) >= 1) AND (length(btrim(activated_by)) <= 128)) AND (activated_by = btrim(activated_by)) AND ((length(btrim(reason)) >= 1) AND (length(btrim(reason)) <= 1024)) AND (reason = btrim(reason))))
  CONSTRAINT chk_source_observation_replay_epoch_singleton CHECK (singleton)
  CONSTRAINT source_observation_replay_epoch_pkey PRIMARY KEY (singleton)

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
  OPTIONS autovacuum_vacuum_scale_factor=0.05,autovacuum_vacuum_threshold=500
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
  COLUMN evidence_sha256 text NOT NULL
  COLUMN collector_instance text NOT NULL
  COLUMN job_key text NOT NULL
  COLUMN collection_job_kind text NOT NULL
  COLUMN fence_epoch bigint NOT NULL
  COLUMN projection_generation bigint NOT NULL
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN payload_id bigint NOT NULL
  CONSTRAINT chk_source_observation_completeness_vocab CHECK ((completeness = ANY (ARRAY['COMPLETE'::text, 'PARTIAL'::text, 'UNKNOWN'::text])))
  CONSTRAINT chk_source_observation_continuity_vocab CHECK ((continuity = ANY (ARRAY['CONTIGUOUS'::text, 'GAP_UNRESOLVED'::text, 'NOT_APPLICABLE'::text])))
  CONSTRAINT chk_source_observation_hashes CHECK (((scope_sha256 ~ '^[0-9a-f]{64}$'::text) AND (evidence_sha256 ~ '^[0-9a-f]{64}$'::text)))
  CONSTRAINT chk_source_observation_text_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(observation_key) >= 1) AND (length(observation_key) <= 512)) AND ((length(collector_instance) >= 1) AND (length(collector_instance) <= 128)) AND ((length(job_key) >= 1) AND (length(job_key) <= 512)) AND ((length(collection_job_kind) >= 1) AND (length(collection_job_kind) <= 128))))
  CONSTRAINT source_observations_contract_generation_check CHECK ((contract_generation > 0))
  CONSTRAINT source_observations_fence_epoch_check CHECK ((fence_epoch > 0))
  CONSTRAINT source_observations_projection_generation_check CHECK ((projection_generation > 0))
  CONSTRAINT source_observations_schema_version_check CHECK ((schema_version > 0))
  CONSTRAINT fk_source_observation_contract FOREIGN KEY (provider, observation_kind) REFERENCES observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT
  CONSTRAINT fk_source_observation_payload FOREIGN KEY (payload_id) REFERENCES source_observation_payloads(id) ON DELETE RESTRICT
  CONSTRAINT source_observations_pkey PRIMARY KEY (id)
  CONSTRAINT uq_source_observation_identity UNIQUE (provider, observation_kind, subject_key, observation_key, schema_version, contract_generation)
  INDEX CREATE INDEX idx_source_observations_kind_received_id ON public.source_observations USING btree (observation_kind, received_at, id)
  INDEX CREATE INDEX idx_source_observations_payload_id ON public.source_observations USING btree (payload_id)

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

TABLE x_space_login_attempts
  COLUMN id bigint NOT NULL GENERATED ALWAYS AS IDENTITY
  COLUMN configuration_revision bigint NOT NULL
  COLUMN session_revision bigint NOT NULL
  COLUMN submitted_revision bigint
  COLUMN status text NOT NULL
  COLUMN error_code text NOT NULL DEFAULT ''::text
  COLUMN started_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN finished_at timestamp with time zone
  CONSTRAINT chk_x_space_login_attempts_error_code_vocab CHECK ((error_code = ANY (ARRAY[''::text, 'additional_authentication'::text, 'login_rejected'::text, 'browser_failed'::text, 'outcome_unknown'::text, 'interrupted'::text, 'candidate_rejected'::text, 'manual_override'::text])))
  CONSTRAINT chk_x_space_login_attempts_status_vocab CHECK ((status = ANY (ARRAY['running'::text, 'submitted'::text, 'connected'::text, 'login_required'::text, 'outcome_unknown'::text, 'manual_override'::text])))
  CONSTRAINT x_space_login_attempts_configuration_revision_check CHECK ((configuration_revision > 0))
  CONSTRAINT x_space_login_attempts_session_revision_check CHECK ((session_revision >= 0))
  CONSTRAINT x_space_login_attempts_pkey PRIMARY KEY (id)
  CONSTRAINT uq_x_space_login_attempts_generation UNIQUE (configuration_revision, session_revision)
  INDEX CREATE INDEX idx_x_space_login_attempts_started_at ON public.x_space_login_attempts USING btree (started_at)

TABLE x_space_session
  COLUMN id integer NOT NULL
  COLUMN revision bigint NOT NULL DEFAULT 0
  COLUMN active_revision bigint NOT NULL DEFAULT 0
  COLUMN active_ciphertext bytea
  COLUMN candidate_ciphertext bytea
  COLUMN state text NOT NULL DEFAULT 'unconfigured'::text
  COLUMN candidate_state text NOT NULL DEFAULT 'idle'::text
  COLUMN last_error text NOT NULL DEFAULT ''::text
  COLUMN candidate_error text NOT NULL DEFAULT ''::text
  COLUMN last_checked_at timestamp with time zone
  COLUMN last_success_at timestamp with time zone
  COLUMN next_check_at timestamp with time zone
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_x_space_session_candidate_state_vocab CHECK ((candidate_state = ANY (ARRAY['idle'::text, 'pending'::text, 'accepted'::text, 'rejected'::text])))
  CONSTRAINT chk_x_space_session_state_vocab CHECK ((state = ANY (ARRAY['unconfigured'::text, 'connected'::text, 'auth_required'::text, 'rate_limited'::text, 'error'::text])))
  CONSTRAINT x_space_session_id_check CHECK ((id = 1))
  CONSTRAINT x_space_session_pkey PRIMARY KEY (id)

TABLE x_space_starts
  COLUMN space_id character varying(64) NOT NULL
  COLUMN payload jsonb NOT NULL
  COLUMN first_seen_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT x_space_starts_pkey PRIMARY KEY (space_id)
  INDEX CREATE INDEX idx_x_space_starts_first_seen ON public.x_space_starts USING btree (first_seen_at)

TABLE youtube_channel_latest_stats
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN member_name text
  COLUMN subscribers bigint
  COLUMN videos bigint
  COLUMN views bigint
  COLUMN time timestamp with time zone NOT NULL
  COLUMN updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
  CONSTRAINT youtube_channel_latest_stats_pkey PRIMARY KEY (channel_id)

TABLE youtube_channel_live_checks
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN provider text NOT NULL
  COLUMN outcome text NOT NULL
  COLUMN selected_video_id character varying(20)
  COLUMN channel_identity_confirmed boolean NOT NULL
  COLUMN unknown_reason text
  COLUMN observation_id bigint
  COLUMN evidence_sha256 text NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN observed_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_channel_live_checks_effective_clock CHECK ((effective_at = scheduled_for))
  CONSTRAINT chk_youtube_channel_live_checks_hash CHECK ((evidence_sha256 ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT chk_youtube_channel_live_checks_identity CHECK ((((length((channel_id)::text) >= 1) AND (length((channel_id)::text) <= 64)) AND ((selected_video_id IS NULL) OR ((length((selected_video_id)::text) >= 1) AND (length((selected_video_id)::text) <= 20)))))
  CONSTRAINT chk_youtube_channel_live_checks_outcome_shape CHECK ((((outcome = 'UNKNOWN'::text) = (unknown_reason IS NOT NULL)) AND ((outcome = 'UNKNOWN'::text) OR channel_identity_confirmed) AND ((outcome <> 'CHANNEL_PAGE'::text) OR (selected_video_id IS NULL)) AND ((outcome <> ALL (ARRAY['LIVE_VIDEO'::text, 'UPCOMING_VIDEO'::text])) OR (selected_video_id IS NOT NULL)) AND ((unknown_reason IS NULL) OR (unknown_reason <> ALL (ARRAY['identity_missing'::text, 'identity_mismatch'::text])) OR (NOT channel_identity_confirmed))))
  CONSTRAINT chk_youtube_channel_live_checks_outcome_vocab CHECK ((outcome = ANY (ARRAY['LIVE_VIDEO'::text, 'UPCOMING_VIDEO'::text, 'CHANNEL_PAGE'::text, 'UNKNOWN'::text])))
  CONSTRAINT chk_youtube_channel_live_checks_provider CHECK ((provider = 'youtubejs'::text))
  CONSTRAINT chk_youtube_channel_live_checks_unknown_reason_vocab CHECK (((unknown_reason IS NULL) OR (unknown_reason = ANY (ARRAY['identity_missing'::text, 'identity_mismatch'::text, 'contradictory_fields'::text, 'structure_unrecognized'::text, 'not_waiting_state'::text, 'login_required_unclassified'::text, 'error_unclassified'::text, 'request_failed'::text]))))
  CONSTRAINT fk_youtube_channel_live_checks_observation FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT youtube_channel_live_checks_pkey PRIMARY KEY (channel_id)
  CONSTRAINT uq_youtube_channel_live_checks_observation UNIQUE (observation_id)

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
  COLUMN last_failure_code text
  COLUMN last_failure_class text
  COLUMN last_failure_detail text
  COLUMN last_failure_at timestamp with time zone
  CONSTRAINT chk_youtube_collection_job_identity CHECK ((((length(job_key) >= 1) AND (length(job_key) <= 512)) AND ((length(collection_job_kind) >= 1) AND (length(collection_job_kind) <= 128)) AND ((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((owner_instance IS NULL) OR ((length(owner_instance) >= 1) AND (length(owner_instance) <= 128))) AND ((last_error_code IS NULL) OR ((length(last_error_code) >= 1) AND (length(last_error_code) <= 128)))))
  CONSTRAINT chk_youtube_collection_job_last_failure_shape CHECK ((((last_failure_code IS NULL) AND (last_failure_class IS NULL) AND (last_failure_detail IS NULL) AND (last_failure_at IS NULL)) OR ((last_failure_code IS NOT NULL) AND ((length(last_failure_code) >= 1) AND (length(last_failure_code) <= 128)) AND (last_failure_class IS NOT NULL) AND (last_failure_class ~ '^[A-Za-z][A-Za-z0-9_]{0,63}$'::text) AND (last_failure_detail IS NOT NULL) AND (octet_length(last_failure_detail) <= 2048) AND (last_failure_at IS NOT NULL))))
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
  CONSTRAINT chk_youtube_collection_target_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'viewer_sample'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text, 'channel_live_check'::text, 'video_live_check'::text])))
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
  CONSTRAINT chk_youtube_community_shorts_alarm_states_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text])))
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
  CONSTRAINT chk_youtube_community_shorts_source_posts_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text])))
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
  CONSTRAINT chk_youtube_content_alarm_tracking_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text])))
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

TABLE youtube_live_absence_slots
  COLUMN observation_id bigint NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN evidence_sha256 text NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  COLUMN scope_sha256 text NOT NULL
  COLUMN coverage jsonb NOT NULL
  CONSTRAINT chk_youtube_live_absence_slots_coverage CHECK (((jsonb_typeof(coverage) = 'object'::text) AND (jsonb_typeof((coverage -> 'requested_channel_ids'::text)) = 'array'::text)))
  CONSTRAINT youtube_live_absence_slots_pkey PRIMARY KEY (observation_id)
  INDEX CREATE INDEX idx_youtube_live_absence_slots_channels ON public.youtube_live_absence_slots USING gin (((coverage -> 'requested_channel_ids'::text)))
  INDEX CREATE INDEX idx_youtube_live_absence_slots_scheduled_for ON public.youtube_live_absence_slots USING btree (scheduled_for)

TABLE youtube_live_pending_ends
  COLUMN video_id text NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN kind text NOT NULL
  COLUMN observation_id bigint NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN ended_at timestamp with time zone
  COLUMN negative_eligible boolean NOT NULL
  COLUMN scope_covers boolean NOT NULL
  CONSTRAINT chk_youtube_live_pending_ends_kind_vocab CHECK ((kind = ANY (ARRAY['EXPLICIT_END'::text, 'EXPLICIT_CANCEL'::text, 'SCOPED_ABSENCE'::text])))
  CONSTRAINT chk_youtube_live_pending_ends_video_id CHECK (((length(video_id) >= 1) AND (length(video_id) <= 128)))
  CONSTRAINT youtube_live_pending_ends_pkey PRIMARY KEY (video_id)
  CONSTRAINT uq_youtube_live_pending_end_observation UNIQUE (video_id, observation_id)

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
  COLUMN first_absence_scheduled_for timestamp with time zone
  COLUMN second_absence_scheduled_for timestamp with time zone
  COLUMN last_absence_observation_id bigint NOT NULL DEFAULT 0
  COLUMN ignored_absence_scheduled_for timestamp with time zone[] NOT NULL DEFAULT '{}'::timestamp with time zone[]
  CONSTRAINT chk_youtube_live_head_candidate_shape CHECK ((((end_candidate_kind IS NULL) AND (end_candidate_observation_id IS NULL) AND (next_end_check_at IS NULL)) OR ((end_candidate_kind IS NOT NULL) AND (end_candidate_observation_id IS NOT NULL) AND (next_end_check_at IS NOT NULL))))
  CONSTRAINT chk_youtube_live_head_video_id CHECK (((length(video_id) >= 1) AND (length(video_id) <= 128)))
  CONSTRAINT youtube_live_reconciliation_hea_consecutive_absence_slots_check CHECK (((consecutive_absence_slots >= 0) AND (consecutive_absence_slots <= 32767)))
  CONSTRAINT youtube_live_reconciliation_heads_end_candidate_kind_check CHECK ((end_candidate_kind = ANY (ARRAY['EXPLICIT_END'::text, 'EXPLICIT_CANCEL'::text, 'SCOPED_ABSENCE'::text])))
  CONSTRAINT youtube_live_reconciliation_heads_end_reason_check CHECK ((end_reason = ANY (ARRAY['EXPLICIT_END'::text, 'CANCELLED_BEFORE_LIVE'::text, 'SCOPED_ABSENCE'::text])))
  CONSTRAINT youtube_live_reconciliation_heads_status_check CHECK ((status = ANY (ARRAY['UPCOMING'::text, 'LIVE'::text, 'ENDED'::text])))
  CONSTRAINT fk_youtube_live_head_pending_end FOREIGN KEY (video_id, end_candidate_observation_id) REFERENCES youtube_live_pending_ends(video_id, observation_id) DEFERRABLE INITIALLY DEFERRED
  CONSTRAINT youtube_live_reconciliation_heads_pkey PRIMARY KEY (video_id)
  INDEX CREATE INDEX idx_youtube_live_reconciliation_due ON public.youtube_live_reconciliation_heads USING btree (next_end_check_at, video_id) WHERE (next_end_check_at IS NOT NULL)
  INDEX CREATE INDEX idx_youtube_live_reconciliation_end_candidate ON public.youtube_live_reconciliation_heads USING btree (end_candidate_observation_id) WHERE (end_candidate_observation_id IS NOT NULL)
  INDEX CREATE INDEX idx_youtube_live_reconciliation_heads_active_video ON public.youtube_live_reconciliation_heads USING btree (video_id) WHERE (status = ANY (ARRAY['LIVE'::text, 'UPCOMING'::text]))

TABLE youtube_live_review_receipts
  COLUMN receipt_id uuid NOT NULL
  COLUMN video_id text NOT NULL
  COLUMN snapshot_sha256 text NOT NULL
  COLUMN original_snapshot jsonb NOT NULL
  COLUMN evidence_refs jsonb NOT NULL
  COLUMN disposition text NOT NULL
  COLUMN operator_id text NOT NULL
  COLUMN reason text NOT NULL
  COLUMN recorded_at timestamp with time zone NOT NULL DEFAULT clock_timestamp()
  CONSTRAINT youtube_live_review_receipts_disposition_check CHECK ((disposition = 'closed_unresolved'::text))
  CONSTRAINT youtube_live_review_receipts_operator_id_check CHECK ((((length(operator_id) >= 1) AND (length(operator_id) <= 128)) AND (operator_id = btrim(operator_id)) AND (operator_id !~ '[[:cntrl:]]'::text)))
  CONSTRAINT youtube_live_review_receipts_original_snapshot_check CHECK ((octet_length((original_snapshot)::text) <= 262144))
  CONSTRAINT youtube_live_review_receipts_reason_check CHECK ((((length(reason) >= 1) AND (length(reason) <= 1024)) AND (reason = btrim(reason)) AND (reason !~ '[[:cntrl:]]'::text)))
  CONSTRAINT youtube_live_review_receipts_snapshot_sha256_check CHECK ((snapshot_sha256 ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT youtube_live_review_receipts_pkey PRIMARY KEY (receipt_id)
  CONSTRAINT youtube_live_review_receipts_video_id_snapshot_sha256_key UNIQUE (video_id, snapshot_sha256)
  TRIGGER CREATE TRIGGER youtube_live_review_receipts_immutable BEFORE DELETE OR UPDATE ON youtube_live_review_receipts FOR EACH ROW EXECUTE FUNCTION reject_youtube_live_review_receipt_change()

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
  COLUMN lifecycle_origin text NOT NULL DEFAULT 'legacy_unknown'::text
  COLUMN status_observed_at timestamp with time zone
  COLUMN schedule_observed_at timestamp with time zone
  CONSTRAINT chk_youtube_live_sessions_lifecycle_origin_vocab CHECK ((lifecycle_origin = ANY (ARRAY['metadata_only'::text, 'observed'::text, 'legacy_unknown'::text])))
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
  CONSTRAINT youtube_live_viewer_sample_evidence_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE CASCADE
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
  COLUMN row_version bigint NOT NULL DEFAULT 0
  COLUMN send_request_id text
  CONSTRAINT chk_youtube_notification_delivery_row_version CHECK (((row_version IS NOT NULL) AND (row_version >= 0)))
  CONSTRAINT chk_youtube_notification_delivery_status_vocab CHECK ((status = ANY (ARRAY[('PENDING'::character varying)::text, ('SENDING'::character varying)::text, ('SENT'::character varying)::text, ('FAILED'::character varying)::text, ('QUARANTINED'::character varying)::text])))
  CONSTRAINT youtube_notification_delivery_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES youtube_notification_outbox(id) ON DELETE CASCADE
  CONSTRAINT youtube_notification_delivery_send_request_id_fkey FOREIGN KEY (send_request_id) REFERENCES youtube_notification_send_request(base_id)
  CONSTRAINT youtube_notification_delivery_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX idx_ynd_outbox_room ON public.youtube_notification_delivery USING btree (outbox_id, room_id)
  INDEX CREATE INDEX idx_ynd_pending_due_created_id ON public.youtube_notification_delivery USING btree (next_attempt_at, created_at, id) WHERE (status = 'PENDING'::text)
  INDEX CREATE INDEX idx_ynd_sending_stale ON public.youtube_notification_delivery USING btree (locked_at, id) WHERE (status = 'SENDING'::text)
  INDEX CREATE INDEX idx_youtube_delivery_send_request ON public.youtube_notification_delivery USING btree (send_request_id) WHERE (send_request_id IS NOT NULL)

TABLE youtube_notification_delivery_ledger
  COLUMN kind text NOT NULL
  COLUMN logical_id character varying(50) NOT NULL
  COLUMN room_id character varying(100) NOT NULL
  COLUMN status text NOT NULL
  COLUMN first_recorded_at timestamp with time zone NOT NULL
  COLUMN updated_at timestamp with time zone NOT NULL
  COLUMN sent_at timestamp with time zone
  COLUMN quarantined_at timestamp with time zone
  COLUMN source_delivery_id bigint
  CONSTRAINT chk_youtube_notification_delivery_ledger_identity CHECK (((length(btrim((logical_id)::text)) > 0) AND (length(btrim((room_id)::text)) > 0) AND ((logical_id)::text = btrim((logical_id)::text)) AND ((room_id)::text = btrim((room_id)::text))))
  CONSTRAINT chk_youtube_notification_delivery_ledger_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text])))
  CONSTRAINT chk_youtube_notification_delivery_ledger_shape CHECK ((((status = 'SENT'::text) AND (sent_at IS NOT NULL)) OR ((status = 'QUARANTINED'::text) AND (sent_at IS NULL) AND (quarantined_at IS NOT NULL))))
  CONSTRAINT chk_youtube_notification_delivery_ledger_source CHECK (((source_delivery_id IS NULL) OR (source_delivery_id > 0)))
  CONSTRAINT chk_youtube_notification_delivery_ledger_status CHECK ((status = ANY (ARRAY['SENT'::text, 'QUARANTINED'::text])))
  CONSTRAINT chk_youtube_notification_delivery_ledger_time_order CHECK (((updated_at >= first_recorded_at) AND ((sent_at IS NULL) OR (sent_at >= first_recorded_at)) AND ((quarantined_at IS NULL) OR (quarantined_at >= first_recorded_at))))
  CONSTRAINT youtube_notification_delivery_ledger_pkey PRIMARY KEY (kind, logical_id, room_id)

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
  COLUMN terminal_at timestamp with time zone
  CONSTRAINT chk_youtube_notification_outbox_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text])))
  CONSTRAINT chk_youtube_notification_outbox_status_vocab CHECK ((status = ANY (ARRAY[('PENDING'::character varying)::text, ('SENT'::character varying)::text, ('FAILED'::character varying)::text])))
  CONSTRAINT youtube_notification_outbox_pkey PRIMARY KEY (id)
  INDEX CREATE UNIQUE INDEX idx_yno_kind_content ON public.youtube_notification_outbox USING btree (kind, content_id)
  INDEX CREATE INDEX idx_yno_pending_due_created_id ON public.youtube_notification_outbox USING btree (next_attempt_at, created_at, id) WHERE (status = 'PENDING'::text)
  INDEX CREATE INDEX idx_yno_status_created ON public.youtube_notification_outbox USING btree (status, created_at)

TABLE youtube_notification_send_request
  COLUMN base_id text NOT NULL
  COLUMN room_id character varying(100) NOT NULL
  COLUMN message text NOT NULL
  COLUMN message_hash text NOT NULL
  COLUMN route text NOT NULL
  COLUMN dedupe_keys text[] NOT NULL
  COLUMN member_ids bigint[] NOT NULL
  COLUMN generation integer NOT NULL DEFAULT 0
  COLUMN created_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT youtube_notification_send_request_dedupe_keys_check CHECK ((cardinality(dedupe_keys) > 0))
  CONSTRAINT youtube_notification_send_request_generation_check CHECK (((generation >= 0) AND (generation <= 2)))
  CONSTRAINT youtube_notification_send_request_member_ids_check CHECK ((cardinality(member_ids) > 0))
  CONSTRAINT youtube_notification_send_request_route_check CHECK ((route = ANY (ARRAY['text'::text, 'markdown'::text, 'sender'::text])))
  CONSTRAINT youtube_notification_send_request_pkey PRIMARY KEY (base_id)

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
  COLUMN collabo_talent_names text[] NOT NULL DEFAULT '{}'::text[]
  CONSTRAINT chk_youtube_schedule_item_bounds CHECK ((((length(group_key) >= 1) AND (length(group_key) <= 256)) AND ((length(external_id) >= 1) AND (length(external_id) <= 256)) AND (length(video_id) <= 128) AND (length(channel_id) <= 256) AND ((length(title) >= 1) AND (length(title) <= 4096))))
  CONSTRAINT chk_youtube_schedule_item_collabo_talent_names CHECK (youtube_schedule_collabo_talent_names_valid(collabo_talent_names))
  CONSTRAINT chk_youtube_schedule_item_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text])))
  CONSTRAINT youtube_schedule_items_pkey PRIMARY KEY (group_key, provider, external_id)

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

TABLE youtube_video_availability
  COLUMN video_id character varying(20) NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN provider text NOT NULL
  COLUMN identity_confirmed boolean NOT NULL
  COLUMN availability text NOT NULL
  COLUMN method text NOT NULL
  COLUMN unknown_reason text
  COLUMN observation_id bigint
  COLUMN evidence_sha256 text NOT NULL
  COLUMN scheduled_for timestamp with time zone NOT NULL
  COLUMN effective_at timestamp with time zone NOT NULL
  COLUMN observed_at timestamp with time zone NOT NULL
  COLUMN received_at timestamp with time zone NOT NULL
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_video_availability_availability_vocab CHECK ((availability = ANY (ARRAY['PUBLIC'::text, 'MEMBERS_ONLY'::text, 'PUBLIC_UNAVAILABLE'::text, 'UNKNOWN'::text])))
  CONSTRAINT chk_youtube_video_availability_effective_clock CHECK ((effective_at = scheduled_for))
  CONSTRAINT chk_youtube_video_availability_hash CHECK ((evidence_sha256 ~ '^[0-9a-f]{64}$'::text))
  CONSTRAINT chk_youtube_video_availability_identity CHECK ((((length((video_id)::text) >= 1) AND (length((video_id)::text) <= 20)) AND ((length((channel_id)::text) >= 1) AND (length((channel_id)::text) <= 64))))
  CONSTRAINT chk_youtube_video_availability_method_vocab CHECK ((method = ANY (ARRAY['player_public'::text, 'player_members_only'::text, 'player_private'::text, 'unknown'::text])))
  CONSTRAINT chk_youtube_video_availability_provider CHECK ((provider = 'youtubejs'::text))
  CONSTRAINT chk_youtube_video_availability_shape CHECK (((((availability = 'PUBLIC'::text) AND (method = 'player_public'::text)) OR ((availability = 'MEMBERS_ONLY'::text) AND (method = 'player_members_only'::text)) OR ((availability = 'PUBLIC_UNAVAILABLE'::text) AND (method = 'player_private'::text)) OR ((availability = 'UNKNOWN'::text) AND (method = 'unknown'::text))) AND ((availability = 'UNKNOWN'::text) = (unknown_reason IS NOT NULL)) AND ((availability = 'UNKNOWN'::text) OR identity_confirmed) AND ((unknown_reason IS DISTINCT FROM 'availability_unclassified'::text) OR identity_confirmed) AND ((unknown_reason IS NULL) OR (unknown_reason <> ALL (ARRAY['identity_missing'::text, 'identity_mismatch'::text])) OR (NOT identity_confirmed))))
  CONSTRAINT chk_youtube_video_availability_unknown_reason_vocab CHECK (((unknown_reason IS NULL) OR (unknown_reason = ANY (ARRAY['identity_missing'::text, 'identity_mismatch'::text, 'contradictory_fields'::text, 'structure_unrecognized'::text, 'not_waiting_state'::text, 'login_required_unclassified'::text, 'error_unclassified'::text, 'request_failed'::text, 'availability_unclassified'::text]))))
  CONSTRAINT fk_youtube_video_availability_observation FOREIGN KEY (observation_id) REFERENCES source_observations(id) ON DELETE SET NULL
  CONSTRAINT youtube_video_availability_pkey PRIMARY KEY (video_id)
  CONSTRAINT uq_youtube_video_availability_observation UNIQUE (observation_id)

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

SEQUENCE bot_reply_outbox_resolution_audit_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY bot_reply_outbox_resolution_audit.id

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

SEQUENCE source_observation_payloads_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_observation_payloads.id

SEQUENCE source_observation_replay_requests_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_observation_replay_requests.id

SEQUENCE source_observations_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_observations.id

SEQUENCE source_reconciliation_conflicts_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY source_reconciliation_conflicts.id

SEQUENCE x_space_login_attempts_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY x_space_login_attempts.id

SEQUENCE youtube_collection_projection_generations_generation_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY youtube_collection_projection_generations.generation

SEQUENCE youtube_notification_delivery_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY youtube_notification_delivery.id

SEQUENCE youtube_notification_delivery_telemetry_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY youtube_notification_delivery_telemetry.id

SEQUENCE youtube_notification_outbox_id_seq AS bigint START 1 INCREMENT 1 MIN 1 MAX 9223372036854775807 CACHE 1 CYCLE false OWNED BY youtube_notification_outbox.id

FUNCTION alarm_dispatch_closeout_snapshot(p_delivery_id bigint) RETURNS TABLE(send_unit_id bigint, target_ids bigint[], target_revisions jsonb, status_metadata jsonb, original_sha256 text, member_count bigint) LANGUAGE sql VOLATILITY s SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\n    WITH target AS (\n        SELECT d.send_unit_id FROM public.alarm_dispatch_deliveries d WHERE d.id = p_delivery_id\n    ), members AS (\n        SELECT d.id, d.updated_at, d.status, d.attempt_count, d.last_error_code,\n               d.quarantined_at, d.sent_at, d.cancelled_at,\n               to_jsonb(d) AS delivery_facts, to_jsonb(e) AS event_facts,\n               to_jsonb(u) AS unit_facts\n        FROM public.alarm_dispatch_deliveries d\n        JOIN target t ON d.send_unit_id = t.send_unit_id\n        JOIN public.alarm_dispatch_events e ON e.id = d.event_id\n        JOIN public.alarm_dispatch_send_units u ON u.id = d.send_unit_id\n        ORDER BY d.id LIMIT 101\n    ), aggregate AS (\n        SELECT count(id) AS member_count,\n               array_agg(id ORDER BY id) AS target_ids,\n               jsonb_agg(jsonb_build_object('id', id::text, 'updatedAt', updated_at) ORDER BY id) AS target_revisions,\n               jsonb_agg(jsonb_build_object(\n                   'id', id::text, 'status', status, 'attemptCount', attempt_count,\n                   'lastErrorCode', CASE\n                       WHEN last_error_code ~ '^[A-Za-z0-9_.:-]{1,128}$' THEN last_error_code\n                       WHEN last_error_code = '' THEN '' ELSE 'unclassified' END,\n                   'updatedAt', updated_at,\n                   'quarantinedAt', quarantined_at, 'sentAt', sent_at, 'cancelledAt', cancelled_at\n               ) ORDER BY id) AS status_metadata,\n               jsonb_agg(jsonb_build_object(\n                   'delivery', delivery_facts,\n                   'event', event_facts, 'sendUnit', unit_facts\n               ) ORDER BY id) AS original_facts\n        FROM members m\n    )\n    SELECT (SELECT t.send_unit_id FROM target t), a.target_ids, a.target_revisions,\n           a.status_metadata, encode(sha256(convert_to(a.original_facts::text, 'UTF8')), 'hex'),\n           a.member_count\n    FROM aggregate a\n    WHERE a.member_count > 0\n"

FUNCTION append_bot_reply_outbox_replay_claim_audit() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nDECLARE\n    granted_actor TEXT;\n    granted_reason TEXT;\nBEGIN\n    IF NEW.status = 'submitting'\n        AND OLD.status <> 'submitting'\n        AND NEW.operator_replay_grants > 0\n    THEN\n        SELECT actor, reason\n        INTO granted_actor, granted_reason\n        FROM public.bot_reply_outbox_replay_audit\n        WHERE outbox_id = NEW.id\n          AND grant_number = NEW.operator_replay_grants\n          AND event_type = 'granted';\n\n        IF NOT FOUND THEN\n            RAISE EXCEPTION 'manual replay grant audit is missing for outbox %, grant %',\n                NEW.id, NEW.operator_replay_grants\n                USING ERRCODE = '23514';\n        END IF;\n\n        INSERT INTO public.bot_reply_outbox_replay_audit (\n            outbox_id, grant_number, event_type, actor, reason\n        ) VALUES (\n            NEW.id, NEW.operator_replay_grants, 'replayed', granted_actor, granted_reason\n        )\n        ON CONFLICT (outbox_id, grant_number, event_type) DO NOTHING;\n    END IF;\n\n    RETURN NEW;\nEND\n"

FUNCTION assert_source_observation_payload_count(actual bigint, expected bigint) RETURNS bigint LANGUAGE plpgsql VOLATILITY i SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nBEGIN\n    IF actual <> expected THEN\n        RAISE EXCEPTION 'source observation payload resolution incomplete: % of %', actual, expected;\n    END IF;\n    RETURN actual;\nEND\n"

FUNCTION assert_source_observation_payload_match(requested_id bigint, stored_payload jsonb, expected_payload jsonb) RETURNS bigint LANGUAGE plpgsql VOLATILITY i SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nBEGIN\n    IF stored_payload IS DISTINCT FROM expected_payload THEN\n        RAISE EXCEPTION 'source observation payload digest collision or content mismatch';\n    END IF;\n    RETURN requested_id;\nEND\n"

FUNCTION delete_retired_youtube_collection_job_leases(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_job_key text) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    WITH candidate AS (\n        SELECT lease.job_key\n        FROM public.youtube_collection_job_leases AS lease\n        JOIN public.youtube_collection_projection_generations AS generation\n          ON generation.generation = lease.projection_generation\n        WHERE generation.status = 'RETIRED'\n          AND generation.valid_until < requested_cutoff\n          AND (\n              lease.slot_state <> 'ACTIVE'\n              OR lease.lease_expires_at < clock_timestamp()\n          )\n        ORDER BY generation.generation, lease.job_key\n        LIMIT CASE\n            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n            ELSE 0\n        END\n        FOR UPDATE OF lease SKIP LOCKED\n    )\n    DELETE FROM public.youtube_collection_job_leases AS lease\n    USING candidate\n    WHERE lease.job_key = candidate.job_key\n    RETURNING lease.job_key\n"

FUNCTION delete_retired_youtube_projection_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_reasons bigint, deleted_targets bigint, deleted_generations bigint) LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nDECLARE\n    chosen_generation BIGINT;\n    locked_target_ctids TID[];\nBEGIN\n    deleted_reasons := 0;\n    deleted_targets := 0;\n    deleted_generations := 0;\n    IF requested_cutoff IS NULL OR requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000 THEN\n        RAISE EXCEPTION 'invalid projection retention request' USING ERRCODE = '22023';\n    END IF;\n\n    -- generation의 UPDATE 잠금은 새 lease의 FK KEY SHARE와 충돌합니다.\n    -- 잠금을 얻는 동안 commit된 lease가 있으면 새 문장의 snapshot으로 다시 확인하여\n    -- 아직 참조되는 generation의 하위 데이터를 지우지 않습니다.\n    SELECT generation.generation INTO chosen_generation\n    FROM public.youtube_collection_projection_generations AS generation\n    WHERE generation.status = 'RETIRED'\n      AND generation.valid_until < requested_cutoff\n      AND NOT EXISTS (\n          SELECT 1 FROM public.youtube_collection_job_leases AS lease\n          WHERE lease.projection_generation = generation.generation\n      )\n    ORDER BY generation.valid_until, generation.generation\n    LIMIT 1\n    FOR UPDATE OF generation SKIP LOCKED;\n\n    IF chosen_generation IS NULL OR EXISTS (\n        SELECT 1 FROM public.youtube_collection_job_leases AS lease\n        WHERE lease.projection_generation = chosen_generation\n    ) THEN\n        RETURN NEXT;\n        RETURN;\n    END IF;\n\n    WITH candidates AS (\n        SELECT reason.projection_generation, reason.subject_key,\n               reason.observation_kind, reason.reason_kind, reason.reason_key\n        FROM public.youtube_collection_target_reasons AS reason\n        WHERE reason.projection_generation = chosen_generation\n        ORDER BY reason.subject_key, reason.observation_kind,\n                 reason.reason_kind, reason.reason_key\n        LIMIT requested_limit\n        FOR UPDATE OF reason SKIP LOCKED\n    )\n    DELETE FROM public.youtube_collection_target_reasons AS reason\n    USING candidates AS candidate\n    WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind,\n           reason.reason_kind, reason.reason_key) =\n          (candidate.projection_generation, candidate.subject_key, candidate.observation_kind,\n           candidate.reason_kind, candidate.reason_key);\n    GET DIAGNOSTICS deleted_reasons = ROW_COUNT;\n\n    -- target을 먼저 제한된 수만큼 잠급니다. reason INSERT의 FK KEY SHARE와 충돌하므로,\n    -- 잠금을 기다리는 동안 추가된 reason까지 새 문장의 snapshot으로 확인할 수 있습니다.\n    -- 자식이 없는 target만 지워 FK cascade가 배치 상한을 우회하지 않게 합니다.\n    IF deleted_reasons < requested_limit THEN\n        SELECT pg_catalog.array_agg(candidate.ctid) INTO locked_target_ctids\n        FROM (\n            SELECT target.ctid\n            FROM public.youtube_collection_targets AS target\n            WHERE target.projection_generation = chosen_generation\n              AND NOT EXISTS (\n                  SELECT 1 FROM public.youtube_collection_target_reasons AS reason\n                  WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind) =\n                        (target.projection_generation, target.subject_key, target.observation_kind)\n              )\n            ORDER BY target.subject_key, target.observation_kind\n            LIMIT requested_limit - deleted_reasons\n            FOR UPDATE OF target SKIP LOCKED\n        ) AS candidate;\n\n        DELETE FROM public.youtube_collection_targets AS target\n        WHERE target.projection_generation = chosen_generation\n          AND target.ctid = ANY(locked_target_ctids)\n          AND NOT EXISTS (\n              SELECT 1 FROM public.youtube_collection_target_reasons AS reason\n              WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind) =\n                    (target.projection_generation, target.subject_key, target.observation_kind)\n          );\n        GET DIAGNOSTICS deleted_targets = ROW_COUNT;\n    END IF;\n\n    DELETE FROM public.youtube_collection_projection_generations AS generation\n    WHERE generation.generation = chosen_generation\n      AND generation.status = 'RETIRED'\n      AND generation.valid_until < requested_cutoff\n      AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_job_leases AS lease\n                      WHERE lease.projection_generation = generation.generation)\n      AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_targets AS target\n                      WHERE target.projection_generation = generation.generation);\n    GET DIAGNOSTICS deleted_generations = ROW_COUNT;\n    RETURN NEXT;\nEND\n"

FUNCTION delete_source_collection_checkpoint_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_scope_sha256 text) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    WITH candidates AS (\n        SELECT checkpoint.provider,\n               checkpoint.observation_kind,\n               checkpoint.subject_key,\n               checkpoint.scope_sha256\n        FROM public.source_collection_checkpoints AS checkpoint\n        WHERE checkpoint.updated_at < requested_cutoff\n          AND EXISTS (\n              SELECT 1\n              FROM public.source_collection_checkpoints AS newer\n              WHERE newer.provider = checkpoint.provider\n                AND newer.observation_kind = checkpoint.observation_kind\n                AND newer.subject_key = checkpoint.subject_key\n                AND (newer.updated_at, newer.scope_sha256) >\n                    (checkpoint.updated_at, checkpoint.scope_sha256)\n          )\n        ORDER BY checkpoint.updated_at,\n                 checkpoint.provider,\n                 checkpoint.observation_kind,\n                 checkpoint.subject_key,\n                 checkpoint.scope_sha256\n        LIMIT CASE\n            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n            ELSE 0\n        END\n        FOR UPDATE OF checkpoint SKIP LOCKED\n    )\n    DELETE FROM public.source_collection_checkpoints AS checkpoint\n    USING candidates AS candidate\n    WHERE checkpoint.provider = candidate.provider\n      AND checkpoint.observation_kind = candidate.observation_kind\n      AND checkpoint.subject_key = candidate.subject_key\n      AND checkpoint.scope_sha256 = candidate.scope_sha256\n      AND checkpoint.updated_at < requested_cutoff\n      AND EXISTS (\n          SELECT 1\n          FROM public.source_collection_checkpoints AS newer\n          WHERE newer.provider = checkpoint.provider\n            AND newer.observation_kind = checkpoint.observation_kind\n            AND newer.subject_key = checkpoint.subject_key\n            AND (newer.updated_at, newer.scope_sha256) >\n                (checkpoint.updated_at, checkpoint.scope_sha256)\n      )\n    RETURNING checkpoint.scope_sha256\n"

FUNCTION delete_source_observation_application_retention_batch(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer) RETURNS TABLE(deleted_id bigint) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    WITH policies AS (\n        SELECT requested_kinds[policy.position] AS observation_kind,\n               requested_cutoffs[policy.position] AS cutoff\n        FROM pg_catalog.generate_subscripts(requested_kinds, 1) AS policy(position)\n        WHERE pg_catalog.cardinality(requested_kinds) BETWEEN 1 AND 16\n          AND pg_catalog.cardinality(requested_kinds) = pg_catalog.cardinality(requested_cutoffs)\n    ),\n    per_policy_candidates AS (\n        SELECT candidate.id,\n               candidate.applied_at\n        FROM policies AS policy\n        CROSS JOIN LATERAL (\n            SELECT application.id,\n                   application.applied_at\n            FROM public.source_observation_applications AS application\n            WHERE application.observation_kind = policy.observation_kind\n              AND application.observation_id IS NULL\n              AND application.applied_at < policy.cutoff\n            ORDER BY application.applied_at, application.id\n            LIMIT CASE\n                WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n                ELSE 0\n            END\n            FOR UPDATE OF application SKIP LOCKED\n        ) AS candidate\n    ),\n    candidates AS (\n        SELECT candidate.id\n        FROM per_policy_candidates AS candidate\n        ORDER BY candidate.applied_at, candidate.id\n        LIMIT CASE\n            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n            ELSE 0\n        END\n    )\n    DELETE FROM public.source_observation_applications AS application\n    USING candidates AS candidate\n    WHERE application.id = candidate.id\n      AND application.observation_id IS NULL\n    RETURNING application.id\n"

FUNCTION delete_source_observation_payload_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS bigint LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nDECLARE\n    previous_id bigint;\n    candidate_ids bigint[];\n    deleted bigint;\nBEGIN\n    IF requested_cutoff IS NULL OR requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000 THEN\n        RAISE EXCEPTION 'invalid payload retention request' USING ERRCODE = '22023';\n    END IF;\n    SELECT cursor_id INTO previous_id FROM public.source_observation_payload_gc_state\n    WHERE singleton FOR UPDATE;\n    SELECT array_agg(candidate.id ORDER BY candidate.id) INTO candidate_ids\n    FROM (\n        SELECT payload.id FROM public.source_observation_payloads payload\n        WHERE payload.id > previous_id AND payload.created_at < requested_cutoff\n        ORDER BY payload.id LIMIT requested_limit FOR UPDATE SKIP LOCKED\n    ) candidate;\n    DELETE FROM public.source_observation_payloads payload\n    WHERE payload.id = ANY(candidate_ids)\n      AND NOT EXISTS (SELECT 1 FROM public.source_observations observation WHERE observation.payload_id = payload.id);\n    GET DIAGNOSTICS deleted = ROW_COUNT;\n    UPDATE public.source_observation_payload_gc_state\n    SET cursor_id = coalesce(candidate_ids[array_length(candidate_ids, 1)], 0)\n    WHERE singleton;\n    RETURN deleted;\nEND\n"

FUNCTION delete_source_observation_retention_batch(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer) RETURNS TABLE(deleted_id bigint) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    WITH policies AS (\n        SELECT requested_kinds[policy.position] AS observation_kind,\n               requested_cutoffs[policy.position] AS cutoff\n        FROM pg_catalog.generate_subscripts(requested_kinds, 1) AS policy(position)\n        WHERE pg_catalog.cardinality(requested_kinds) BETWEEN 1 AND 16\n          AND pg_catalog.cardinality(requested_kinds) = pg_catalog.cardinality(requested_cutoffs)\n    ),\n    per_policy_candidates AS (\n        SELECT candidate.id,\n               candidate.received_at\n        FROM policies AS policy\n        CROSS JOIN LATERAL (\n            SELECT observation.id,\n                   observation.received_at\n            FROM public.source_observations AS observation\n            WHERE observation.observation_kind = policy.observation_kind\n              AND observation.received_at < policy.cutoff\n              AND NOT EXISTS (\n                  SELECT 1\n                  FROM public.source_observation_queue AS queue\n                  WHERE queue.observation_id = observation.id\n              )\n              AND NOT EXISTS (\n                  SELECT 1\n                  FROM public.source_observation_replay_requests AS replay\n                  WHERE replay.observation_id = observation.id\n                    AND replay.status = 'PENDING'\n              )\n              AND NOT EXISTS (\n                  SELECT 1\n                  FROM public.youtube_live_reconciliation_heads AS head\n                  WHERE head.end_candidate_observation_id = observation.id\n              )\n            ORDER BY observation.received_at, observation.id\n            LIMIT CASE\n                WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n                ELSE 0\n            END\n            FOR UPDATE OF observation SKIP LOCKED\n        ) AS candidate\n    ),\n    candidates AS (\n        SELECT candidate.id\n        FROM per_policy_candidates AS candidate\n        ORDER BY candidate.received_at, candidate.id\n        LIMIT CASE\n            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n            ELSE 0\n        END\n    )\n    DELETE FROM public.source_observations AS observation\n    USING candidates\n    WHERE observation.id = candidates.id\n      AND NOT EXISTS (\n          SELECT 1\n          FROM public.source_observation_queue AS live_queue\n          WHERE live_queue.observation_id = observation.id\n      )\n      AND NOT EXISTS (\n          SELECT 1\n          FROM public.source_observation_replay_requests AS live_replay\n          WHERE live_replay.observation_id = observation.id\n            AND live_replay.status = 'PENDING'\n      )\n      AND NOT EXISTS (\n          SELECT 1\n          FROM public.youtube_live_reconciliation_heads AS live_head\n          WHERE live_head.end_candidate_observation_id = observation.id\n      )\n    RETURNING observation.id\n"

FUNCTION delete_youtube_live_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_id bigint) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    WITH active_old_live AS (\n        SELECT 1\n        FROM public.source_observation_queue AS queue\n        JOIN public.source_observations AS observation\n          ON observation.id = queue.observation_id\n        WHERE queue.status = 'PENDING'\n          AND observation.observation_kind = 'live_snapshot'\n          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff\n        LIMIT 1\n    ),\n    processing_old_live AS (\n        SELECT 1\n        FROM public.source_observation_queue AS queue\n        JOIN public.source_observations AS observation\n          ON observation.id = queue.observation_id\n        WHERE queue.status = 'PROCESSING'\n          AND observation.observation_kind = 'live_snapshot'\n          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff\n        LIMIT 1\n    ),\n    replaying_old_live AS (\n        SELECT 1\n        FROM public.source_observation_replay_requests AS replay\n        JOIN public.source_observations AS observation\n          ON observation.id = replay.observation_id\n        WHERE replay.status = 'PENDING'\n          AND observation.observation_kind = 'live_snapshot'\n          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff\n        LIMIT 1\n    ),\n    candidates AS (\n        SELECT slot.observation_id\n        FROM public.youtube_live_absence_slots AS slot\n        WHERE slot.scheduled_for < requested_cutoff\n          AND NOT EXISTS (SELECT 1 FROM active_old_live)\n          AND NOT EXISTS (SELECT 1 FROM processing_old_live)\n          AND NOT EXISTS (SELECT 1 FROM replaying_old_live)\n        ORDER BY slot.scheduled_for, slot.observation_id\n        LIMIT CASE\n            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit\n            ELSE 0\n        END\n        FOR UPDATE OF slot SKIP LOCKED\n    )\n    DELETE FROM public.youtube_live_absence_slots AS slot\n    USING candidates AS candidate\n    WHERE slot.observation_id = candidate.observation_id\n      AND slot.scheduled_for < requested_cutoff\n    RETURNING slot.observation_id\n"

FUNCTION discard_bot_reply_outbox_manual_review(requested_outbox_id bigint, operator_actor text, operator_reason text, observed_iris_state text) RETURNS text LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nDECLARE\n    decided_at TIMESTAMPTZ := clock_timestamp();\n    normalized_actor TEXT := btrim(operator_actor);\n    normalized_reason TEXT := btrim(operator_reason);\n    normalized_iris_state TEXT := btrim(observed_iris_state);\n    target_id BIGINT;\n    target_status TEXT;\nBEGIN\n    SELECT id, status\n    INTO target_id, target_status\n    FROM public.bot_reply_outbox\n    WHERE id = requested_outbox_id\n    FOR UPDATE;\n\n    IF NOT FOUND THEN\n        RETURN 'not_found';\n    END IF;\n    IF target_status <> 'manual_review' THEN\n        RETURN 'not_manual_review';\n    END IF;\n    IF normalized_actor IS NULL\n        OR normalized_actor !~ '^[A-Za-z0-9._:@-]{1,64}$'\n        OR normalized_reason IS NULL\n        OR octet_length(normalized_reason) NOT BETWEEN 1 AND 256\n        OR normalized_reason ~ '[[:cntrl:]]'\n    THEN\n        RETURN 'invalid_operator_metadata';\n    END IF;\n    IF normalized_iris_state IS NULL\n        OR normalized_iris_state NOT IN (\n            'queued',\n            'preparing',\n            'prepared',\n            'sending',\n            'handoff_completed',\n            'failed',\n            'outcome_unknown',\n            'not_found'\n        )\n    THEN\n        RETURN 'invalid_iris_state';\n    END IF;\n\n    INSERT INTO public.bot_reply_outbox_resolution_audit (\n        outbox_id,\n        decision,\n        observed_iris_state,\n        actor,\n        reason,\n        recorded_at\n    ) VALUES (\n        target_id,\n        'discarded_without_replay',\n        normalized_iris_state,\n        normalized_actor,\n        normalized_reason,\n        decided_at\n    );\n\n    UPDATE public.bot_reply_outbox\n    SET status = 'discarded',\n        payload = NULL,\n        claim_token = NULL,\n        lease_until = NULL,\n        last_error = 'operator discarded manual review without replay',\n        updated_at = decided_at\n    WHERE id = target_id;\n\n    RETURN 'discarded';\nEND\n"

FUNCTION enforce_bot_reply_outbox_discard_audit() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nBEGIN\n    IF NEW.status <> 'discarded' THEN\n        RETURN NEW;\n    END IF;\n    IF TG_OP = 'INSERT' THEN\n        RAISE EXCEPTION 'discarded reply transition requires an audited manual-review decision'\n            USING ERRCODE = '23514';\n    END IF;\n    IF OLD.status = 'discarded' THEN\n        RETURN NEW;\n    END IF;\n    IF OLD.status <> 'manual_review'\n        OR NEW.payload IS NOT NULL\n        OR NEW.claim_token IS NOT NULL\n        OR NEW.lease_until IS NOT NULL\n        OR NOT EXISTS (\n            SELECT 1\n            FROM public.bot_reply_outbox_resolution_audit AS audit\n            WHERE audit.outbox_id = NEW.id\n              AND audit.decision = 'discarded_without_replay'\n        )\n    THEN\n        RAISE EXCEPTION 'discarded reply transition requires an audited manual-review decision'\n            USING ERRCODE = '23514';\n    END IF;\n\n    RETURN NEW;\nEND\n"

FUNCTION grant_bot_reply_outbox_manual_replay(requested_outbox_id bigint, operator_actor text, operator_reason text) RETURNS text LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nDECLARE\n    granted_at TIMESTAMPTZ := clock_timestamp();\n    normalized_actor TEXT := btrim(operator_actor);\n    normalized_reason TEXT := btrim(operator_reason);\n    target_id BIGINT;\n    target_status TEXT;\n    target_created_at TIMESTAMPTZ;\n    target_replay_grants INTEGER;\n    next_grant_number INTEGER;\nBEGIN\n    SELECT id, status, created_at, operator_replay_grants\n    INTO target_id, target_status, target_created_at, target_replay_grants\n    FROM public.bot_reply_outbox\n    WHERE id = requested_outbox_id\n    FOR UPDATE;\n\n    IF NOT FOUND THEN\n        RETURN 'not_found';\n    END IF;\n    IF target_status <> 'manual_review' THEN\n        RETURN 'not_manual_review';\n    END IF;\n    IF granted_at >= target_created_at + interval '144 hours' THEN\n        RETURN 'cutoff_expired';\n    END IF;\n    IF normalized_actor !~ '^[A-Za-z0-9._:@-]{1,64}$'\n        OR octet_length(normalized_reason) NOT BETWEEN 1 AND 256\n        OR normalized_reason ~ '[[:cntrl:]]'\n    THEN\n        RETURN 'invalid_operator_metadata';\n    END IF;\n\n    next_grant_number := target_replay_grants + 1;\n    INSERT INTO public.bot_reply_outbox_replay_audit (\n        outbox_id, grant_number, event_type, actor, reason, recorded_at\n    ) VALUES (\n        target_id, next_grant_number, 'granted', normalized_actor, normalized_reason, granted_at\n    );\n\n    UPDATE public.bot_reply_outbox\n    SET status = 'pending',\n        claim_token = NULL,\n        lease_until = NULL,\n        last_error = '',\n        operator_replay_grants = next_grant_number,\n        available_at = granted_at,\n        updated_at = granted_at\n    WHERE id = target_id;\n\n    RETURN 'replayed';\nEND\n"

FUNCTION lock_observation_contract(requested_provider text, requested_observation_kind text) RETURNS TABLE(current_schema_version smallint, current_generation bigint) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    SELECT contract.current_schema_version,\n           contract.current_generation\n    FROM public.observation_contract_generations AS contract\n    WHERE contract.provider = requested_provider\n      AND contract.observation_kind = requested_observation_kind\n    FOR SHARE OF contract\n"

FUNCTION lock_source_observation(requested_observation_id bigint) RETURNS TABLE(provider text, observation_kind text, subject_key text, observation_key text, schema_version smallint, contract_generation bigint, evidence_sha256 text) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    SELECT observation.provider,\n           observation.observation_kind,\n           observation.subject_key,\n           observation.observation_key,\n           observation.schema_version,\n           observation.contract_generation,\n           observation.evidence_sha256\n    FROM public.source_observations AS observation\n    WHERE observation.id = requested_observation_id\n    FOR SHARE OF observation\n"

FUNCTION lock_source_observation_identity(requested_provider text, requested_observation_kind text, requested_subject_key text, requested_observation_key text, requested_schema_version smallint, requested_contract_generation bigint) RETURNS TABLE(id bigint, evidence_sha256 text) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    SELECT observation.id,\n           observation.evidence_sha256\n    FROM public.source_observations AS observation\n    WHERE observation.provider = requested_provider\n      AND observation.observation_kind = requested_observation_kind\n      AND observation.subject_key = requested_subject_key\n      AND observation.observation_key = requested_observation_key\n      AND observation.schema_version = requested_schema_version\n      AND observation.contract_generation = requested_contract_generation\n    FOR SHARE OF observation\n"

FUNCTION lock_source_observation_payload(requested_kind text, requested_version smallint, requested_digest bytea, expected_payload jsonb) RETURNS TABLE(id bigint) LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nDECLARE\n    stored_payload jsonb;\nBEGIN\n    SELECT candidate.id, candidate.payload\n    INTO id, stored_payload\n    FROM public.source_observation_payloads AS candidate\n    WHERE candidate.observation_kind = requested_kind\n      AND candidate.schema_version = requested_version\n      AND candidate.canonical_profile = 'source-observation-canonical-json-v1'\n      AND candidate.payload_sha256 = requested_digest\n    FOR KEY SHARE OF candidate;\n    IF FOUND THEN\n        IF stored_payload IS DISTINCT FROM expected_payload THEN\n            RAISE EXCEPTION 'source observation payload digest collision or content mismatch';\n        END IF;\n        RETURN NEXT;\n    END IF;\nEND\n"

FUNCTION lock_youtube_collection_projection(requested_generation bigint) RETURNS TABLE(generation bigint) LANGUAGE sql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\n    SELECT projection.generation\n    FROM public.youtube_collection_projection_generations AS projection\n    WHERE projection.generation = requested_generation\n      AND projection.status = 'CURRENT'\n      AND projection.valid_until > clock_timestamp()\n    FOR SHARE OF projection\n"

FUNCTION notification_template_row_version() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\nBEGIN\n    IF (NEW.body, NEW.template_key, NEW.channel_id, NEW.id)\n        IS DISTINCT FROM (OLD.body, OLD.template_key, OLD.channel_id, OLD.id) THEN\n        NEW.row_version := OLD.row_version + 1;\n    ELSE\n        NEW.row_version := OLD.row_version;\n    END IF;\n    RETURN NEW;\nEND;\n"

FUNCTION record_alarm_dispatch_closeout(p_receipt_id uuid, p_addressed_delivery_id bigint, p_expected_target_ids bigint[], p_expected_sha256 text, p_operator_id text, p_reason text) RETURNS void LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\nDECLARE\n    unit_id bigint;\n    locked_count integer;\n    snapshot record;\nBEGIN\n    IF current_setting('transaction_isolation') <> 'serializable' THEN\n        RAISE EXCEPTION 'alarm dispatch closeout requires serializable transaction';\n    END IF;\n    IF p_receipt_id IS NULL OR p_addressed_delivery_id IS NULL OR p_addressed_delivery_id <= 0\n       OR p_expected_target_ids IS NULL OR cardinality(p_expected_target_ids) NOT BETWEEN 1 AND 100\n       OR p_expected_sha256 IS NULL OR p_expected_sha256 !~ '^[0-9a-f]{64}$'\n       OR p_operator_id IS NULL OR length(btrim(p_operator_id)) NOT BETWEEN 1 AND 128\n       OR p_operator_id <> btrim(p_operator_id) OR p_operator_id ~ '[[:cntrl:]]'\n       OR p_reason IS NULL OR length(btrim(p_reason)) NOT BETWEEN 1 AND 1024\n       OR p_reason <> btrim(p_reason) OR p_reason ~ '[[:cntrl:]]' THEN\n        RAISE EXCEPTION 'invalid alarm dispatch closeout request';\n    END IF;\n    IF array_position(p_expected_target_ids, p_addressed_delivery_id) IS NULL THEN\n        RAISE EXCEPTION 'addressed delivery missing from reviewed targets';\n    END IF;\n\n    -- 대상과 전체 send unit을 requeue와 같은 순서로 잠그고, 잠금 대기 뒤 재조회한다.\n    SELECT d.send_unit_id INTO unit_id\n    FROM public.alarm_dispatch_deliveries d WHERE d.id = p_addressed_delivery_id FOR UPDATE;\n    IF NOT FOUND OR unit_id IS NULL THEN\n        RAISE EXCEPTION 'addressed delivery or send unit missing';\n    END IF;\n    PERFORM d.id FROM public.alarm_dispatch_deliveries d\n    WHERE d.send_unit_id = unit_id ORDER BY d.id LIMIT 101 FOR UPDATE OF d;\n    GET DIAGNOSTICS locked_count = ROW_COUNT;\n    SELECT s.send_unit_id, s.target_ids, s.target_revisions, s.status_metadata,\n           s.original_sha256, s.member_count INTO snapshot\n    FROM public.alarm_dispatch_closeout_snapshot(p_addressed_delivery_id) AS s;\n    IF NOT FOUND OR snapshot.send_unit_id <> unit_id OR snapshot.member_count <> locked_count\n       OR snapshot.member_count > 100\n       OR snapshot.target_ids IS DISTINCT FROM p_expected_target_ids\n       OR snapshot.original_sha256 IS DISTINCT FROM p_expected_sha256 THEN\n        RAISE EXCEPTION 'reviewed closeout snapshot changed';\n    END IF;\n    IF EXISTS (\n        SELECT 1 FROM public.alarm_dispatch_deliveries d\n        WHERE d.id = ANY(snapshot.target_ids)\n          AND (d.status <> 'quarantined' OR d.sent_at IS NOT NULL OR d.cancelled_at IS NOT NULL)\n    ) THEN\n        RAISE EXCEPTION 'closeout requires only quarantined, unsent targets';\n    END IF;\n    IF EXISTS (SELECT 1 FROM public.alarm_dispatch_closeout_receipts r WHERE r.send_unit_id = unit_id) THEN\n        RAISE EXCEPTION 'send unit already has a closeout receipt';\n    END IF;\n    INSERT INTO public.alarm_dispatch_closeout_receipts (\n        receipt_id, send_unit_id, addressed_delivery_id, target_ids,\n        target_revisions, status_metadata, original_sha256, operator_id, reason\n    ) VALUES (\n        p_receipt_id, unit_id, p_addressed_delivery_id, snapshot.target_ids,\n        snapshot.target_revisions, snapshot.status_metadata, snapshot.original_sha256,\n        p_operator_id, p_reason\n    );\nEND\n"

FUNCTION record_youtube_live_review(p_receipt_id uuid, p_video_id text, p_expected_sha256 text, p_operator_id text, p_reason text) RETURNS void LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\nDECLARE\n    snapshot record;\nBEGIN\n    IF current_setting('transaction_isolation') <> 'serializable' THEN\n        RAISE EXCEPTION 'live review requires serializable transaction';\n    END IF;\n    IF p_receipt_id IS NULL OR p_video_id IS NULL OR length(p_video_id) NOT BETWEEN 1 AND 128\n       OR p_expected_sha256 IS NULL OR p_expected_sha256 !~ '^[0-9a-f]{64}$'\n       OR p_operator_id IS NULL OR length(p_operator_id) NOT BETWEEN 1 AND 128\n       OR p_operator_id <> btrim(p_operator_id) OR p_operator_id ~ '[[:cntrl:]]'\n       OR p_reason IS NULL OR length(p_reason) NOT BETWEEN 1 AND 1024\n       OR p_reason <> btrim(p_reason) OR p_reason ~ '[[:cntrl:]]' THEN\n        RAISE EXCEPTION 'invalid live review request';\n    END IF;\n    PERFORM video_id FROM public.youtube_live_sessions WHERE video_id = p_video_id FOR UPDATE;\n    IF NOT FOUND THEN\n        RAISE EXCEPTION 'reviewed live session missing';\n    END IF;\n    PERFORM video_id FROM public.youtube_live_reconciliation_heads WHERE video_id = p_video_id FOR UPDATE;\n    PERFORM video_id FROM public.youtube_live_pending_ends WHERE video_id = p_video_id FOR UPDATE;\n    PERFORM video_id FROM public.youtube_video_availability WHERE video_id = p_video_id FOR UPDATE;\n    SELECT reviewed.original_snapshot,reviewed.snapshot_sha256,reviewed.evidence_refs,reviewed.reviewable\n    INTO snapshot FROM public.youtube_live_review_snapshot(p_video_id) reviewed;\n    IF snapshot.snapshot_sha256 IS DISTINCT FROM p_expected_sha256 OR NOT snapshot.reviewable THEN\n        RAISE EXCEPTION 'reviewed live snapshot changed or is not unresolved';\n    END IF;\n    INSERT INTO public.youtube_live_review_receipts\n        (receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)\n    VALUES (p_receipt_id,p_video_id,snapshot.snapshot_sha256,snapshot.original_snapshot,\n            snapshot.evidence_refs,'closed_unresolved',p_operator_id,p_reason);\nEND\n"

FUNCTION reject_alarm_dispatch_closeout_receipt_change() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\nBEGIN\n    RAISE EXCEPTION 'alarm dispatch closeout receipts are append-only';\nEND\n"

FUNCTION reject_bot_reply_outbox_replay_audit_mutation() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nBEGIN\n    IF TG_OP = 'DELETE'\n        AND NOT EXISTS (\n            SELECT 1\n            FROM public.bot_reply_outbox\n            WHERE id = OLD.outbox_id\n        )\n    THEN\n        RETURN OLD;\n    END IF;\n\n    RAISE EXCEPTION 'bot_reply_outbox_replay_audit events are immutable'\n        USING ERRCODE = '55000';\nEND\n"

FUNCTION reject_bot_reply_outbox_resolution_audit_mutation() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nBEGIN\n    IF TG_OP = 'DELETE'\n        AND NOT EXISTS (\n            SELECT 1\n            FROM public.bot_reply_outbox\n            WHERE id = OLD.outbox_id\n        )\n    THEN\n        RETURN OLD;\n    END IF;\n\n    RAISE EXCEPTION 'bot_reply_outbox_resolution_audit events are immutable'\n        USING ERRCODE = '55000';\nEND\n"

FUNCTION reject_youtube_live_review_receipt_change() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\nBEGIN\n    RAISE EXCEPTION 'live review receipts are append-only';\nEND\n"

FUNCTION require_source_observation_payload(requested_id bigint, requested_kind text, requested_version smallint, actual_kind text, actual_version smallint, actual_profile text, actual_digest bytea, actual_payload jsonb) RETURNS jsonb LANGUAGE plpgsql VOLATILITY s SECURITY_DEFINER true LEAKPROOF false PARALLEL u CONFIG search_path=pg_catalog BODY "\nBEGIN\n    IF actual_payload IS NULL OR actual_kind IS DISTINCT FROM requested_kind\n       OR actual_version IS DISTINCT FROM requested_version\n       OR actual_profile IS DISTINCT FROM 'source-observation-canonical-json-v1'\n       OR actual_digest IS NULL OR octet_length(actual_digest) <> 32 THEN\n        RAISE EXCEPTION 'missing or corrupt source observation payload for observation %', requested_id;\n    END IF;\n    RETURN actual_payload;\nEND\n"

FUNCTION scrub_bot_command_execution_terminal_summary() RETURNS trigger LANGUAGE plpgsql VOLATILITY v SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\nBEGIN\n    NEW.result_summary := NEW.status;\n    RETURN NEW;\nEND\n"

FUNCTION youtube_live_review_snapshot(p_video_id text) RETURNS TABLE(original_snapshot jsonb, snapshot_sha256 text, evidence_refs jsonb, reviewable boolean) LANGUAGE sql VOLATILITY s SECURITY_DEFINER false LEAKPROOF false PARALLEL u BODY "\n    WITH facts AS (\n        SELECT jsonb_build_object('session',to_jsonb(session),'head',to_jsonb(head),\n                   'pending',to_jsonb(pending),'availability',to_jsonb(availability)) AS snapshot,\n               jsonb_build_object('availability_observation_id',availability.observation_id,\n                   'availability_evidence_sha256',availability.evidence_sha256,\n                   'pending_observation_id',pending.observation_id) AS refs,\n               session.status = 'UPCOMING'\n                   AND (head.video_id IS NULL OR head.status = session.status)\n                   AND (session.lifecycle_origin <> 'observed' OR head.video_id IS NOT NULL)\n                   -- 가용성 PUBLIC도 수명 미상일 수 있다. 현재 확인보다 새롭거나\n                   -- 같은 positive가 있으면 확인된 UPCOMING을 unresolved로 닫지 않는다.\n                   AND availability.video_id IS NOT NULL\n                   AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)\n                       >= availability.effective_at,false) AS reviewable\n        FROM public.youtube_live_sessions session\n        LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)\n        LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)\n        LEFT JOIN public.youtube_video_availability availability USING (video_id)\n        WHERE session.video_id = p_video_id\n    )\n    SELECT snapshot, encode(sha256(convert_to(snapshot::TEXT,'UTF8')),'hex'), refs,\n           COALESCE(reviewable,false) FROM facts\n"

FUNCTION youtube_schedule_collabo_talent_names_valid(names text[]) RETURNS boolean LANGUAGE sql VOLATILITY i SECURITY_DEFINER false LEAKPROOF false PARALLEL s CONFIG search_path=pg_catalog BODY "\n    SELECT COALESCE(pg_catalog.array_ndims(names), 1) = 1\n       AND COALESCE(pg_catalog.array_lower(names, 1), 1) = 1\n       AND pg_catalog.cardinality(names) <= 32\n       AND NOT EXISTS (\n           SELECT 1\n           FROM pg_catalog.unnest(names) AS name\n           WHERE name IS NULL\n              OR pg_catalog.octet_length(name) < 1\n              OR pg_catalog.octet_length(name) > 256\n       );\n"
