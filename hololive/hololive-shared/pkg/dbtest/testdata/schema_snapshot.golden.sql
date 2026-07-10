-- hololive schema snapshot (deterministic pg_catalog serialization)
-- objects: enum types, tables, columns, constraints, indexes
-- regenerate: SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-shared/pkg/dbtest

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
  INDEX CREATE INDEX idx_alarm_dispatch_admin_actions_delivery_created ON public.alarm_dispatch_admin_actions USING btree (delivery_id, created_at DESC)

TABLE alarm_dispatch_deliveries
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
  CONSTRAINT alarm_dispatch_deliveries_attempt_check CHECK ((attempt_count >= 0))
  CONSTRAINT alarm_dispatch_deliveries_dedupe_key_check CHECK (((length(dedupe_key) > 0) AND (length(dedupe_key) <= 768)))
  CONSTRAINT alarm_dispatch_deliveries_room_id_check CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100)))
  CONSTRAINT alarm_dispatch_deliveries_status_check CHECK ((status = ANY (ARRAY['shadowed'::text, 'pending'::text, 'retry'::text, 'leased'::text, 'sending'::text, 'sent'::text, 'dlq'::text, 'quarantined'::text, 'cancelled'::text])))
  CONSTRAINT alarm_dispatch_deliveries_event_id_fkey FOREIGN KEY (event_id) REFERENCES alarm_dispatch_events(id) ON DELETE RESTRICT
  CONSTRAINT alarm_dispatch_deliveries_pkey PRIMARY KEY (id)
  CONSTRAINT alarm_dispatch_deliveries_dedupe_key_key UNIQUE (dedupe_key)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_cancelled_retention ON public.alarm_dispatch_deliveries USING btree (cancelled_at, id) WHERE (status = 'cancelled'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_dlq_retention ON public.alarm_dispatch_deliveries USING btree (dlq_at, id) WHERE (status = 'dlq'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_due ON public.alarm_dispatch_deliveries USING btree (next_attempt_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retry'::text]))
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_event_id ON public.alarm_dispatch_deliveries USING btree (event_id)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_leased_expired ON public.alarm_dispatch_deliveries USING btree (lock_expires_at, id) WHERE (status = 'leased'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_quarantined_retention ON public.alarm_dispatch_deliveries USING btree (quarantined_at, id) WHERE (status = 'quarantined'::text)
  INDEX CREATE INDEX idx_alarm_dispatch_deliveries_room_created ON public.alarm_dispatch_deliveries USING btree (room_id, created_at DESC)
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
  INDEX CREATE INDEX idx_alarm_dispatch_event_collisions_status_created ON public.alarm_dispatch_event_collisions USING btree (status, created_at DESC, id DESC)

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
  INDEX CREATE INDEX idx_major_events_link_check ON public.major_events USING btree (link_status, link_checked_at)
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
  INDEX CREATE INDEX idx_members_twitch_user_id ON public.members USING btree (twitch_user_id) WHERE (twitch_user_id IS NOT NULL)

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
  INDEX CREATE INDEX idx_ndo_lease_expired ON public.notification_delivery_outbox USING btree (lock_expires_at) WHERE ((status = 'PENDING'::text) AND (lock_expires_at IS NOT NULL))
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

TABLE youtube_channel_latest_stats
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN member_name text
  COLUMN subscribers bigint
  COLUMN videos bigint
  COLUMN views bigint
  COLUMN time timestamp with time zone NOT NULL
  COLUMN updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
  CONSTRAINT youtube_channel_latest_stats_pkey PRIMARY KEY (channel_id)

TABLE youtube_channel_profiles
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN avatar jsonb
  COLUMN banner jsonb
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT youtube_channel_profiles_pkey PRIMARY KEY (channel_id)

TABLE youtube_channel_stats_snapshots
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN captured_at timestamp with time zone NOT NULL
  COLUMN subscriber_count bigint NOT NULL DEFAULT 0
  COLUMN view_count bigint NOT NULL DEFAULT 0
  COLUMN video_count bigint NOT NULL DEFAULT 0
  COLUMN joined_date bigint
  COLUMN description text
  COLUMN country character varying(50)
  COLUMN handle character varying(100)
  CONSTRAINT youtube_channel_stats_snapshots_pkey PRIMARY KEY (channel_id, captured_at)
  INDEX CREATE INDEX idx_ycss_captured_at_brin ON public.youtube_channel_stats_snapshots USING brin (captured_at)

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
  INDEX CREATE INDEX idx_ycsas_alarm_sent_at ON public.youtube_community_shorts_alarm_states USING btree (alarm_sent_at DESC) WHERE (alarm_sent_at IS NOT NULL)
  INDEX CREATE INDEX idx_ycsas_authorized_at ON public.youtube_community_shorts_alarm_states USING btree (authorized_at DESC) WHERE (authorized_at IS NOT NULL)
  INDEX CREATE INDEX idx_ycsas_channel_detected ON public.youtube_community_shorts_alarm_states USING btree (channel_id, detected_at DESC)
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
  INDEX CREATE INDEX idx_ycssp_detected_at ON public.youtube_community_shorts_source_posts USING btree (detected_at DESC)

TABLE youtube_content_alarm_tracking
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
  INDEX CREATE INDEX idx_ycat_alarm_sent_at ON public.youtube_content_alarm_tracking USING btree (alarm_sent_at) WHERE (alarm_sent_at IS NOT NULL)
  INDEX CREATE INDEX idx_ycat_channel_detected ON public.youtube_content_alarm_tracking USING btree (channel_id, detected_at DESC)
  INDEX CREATE INDEX idx_ycat_delivery_status ON public.youtube_content_alarm_tracking USING btree (delivery_status, detected_at DESC)
  INDEX CREATE INDEX idx_ycat_detected_at ON public.youtube_content_alarm_tracking USING btree (detected_at DESC)
  INDEX CREATE INDEX idx_ycat_kind_content ON public.youtube_content_alarm_tracking USING btree (kind, content_id)

TABLE youtube_content_watermarks
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN watermark_type character varying(20) NOT NULL
  COLUMN initialized boolean NOT NULL DEFAULT false
  COLUMN last_content_id character varying(50)
  COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now()
  CONSTRAINT chk_youtube_content_watermarks_watermark_type_vocab CHECK (((watermark_type)::text = ANY ((ARRAY['VIDEO'::character varying, 'SHORT'::character varying, 'COMMUNITY_POST'::character varying])::text[])))
  CONSTRAINT youtube_content_watermarks_pkey PRIMARY KEY (channel_id, watermark_type)

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
  CONSTRAINT chk_youtube_live_sessions_status_vocab CHECK ((status = ANY (ARRAY[('UPCOMING'::character varying)::text, ('LIVE'::character varying)::text, ('ENDED'::character varying)::text])))
  CONSTRAINT youtube_live_sessions_pkey PRIMARY KEY (video_id)
  INDEX CREATE INDEX idx_yls_channel_last_seen ON public.youtube_live_sessions USING btree (channel_id, last_seen_at DESC)
  INDEX CREATE INDEX idx_yls_ended_channel_sort_video ON public.youtube_live_sessions USING btree (channel_id, COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at) DESC, video_id DESC) WHERE (status = 'ENDED'::text)
  INDEX CREATE INDEX idx_yls_ended_cleanup ON public.youtube_live_sessions USING btree (ended_at, video_id) WHERE ((status = 'ENDED'::text) AND (ended_at IS NOT NULL))
  INDEX CREATE INDEX idx_yls_ended_sort_video ON public.youtube_live_sessions USING btree (COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at) DESC, video_id DESC) WHERE (status = 'ENDED'::text)
  INDEX CREATE INDEX idx_yls_live_first_seen ON public.youtube_live_sessions USING btree (live_first_seen_at, channel_id) WHERE (status = 'LIVE'::text)
  INDEX CREATE INDEX idx_yls_status_last_seen ON public.youtube_live_sessions USING btree (status, last_seen_at DESC)
  INDEX CREATE INDEX idx_yls_status_topic_last_seen ON public.youtube_live_sessions USING btree (status, topic_id, last_seen_at DESC) WHERE (topic_id <> ''::text)

TABLE youtube_live_viewer_samples
  COLUMN video_id character varying(20) NOT NULL
  COLUMN captured_at timestamp with time zone NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN concurrent_viewers integer NOT NULL DEFAULT 0
  CONSTRAINT youtube_live_viewer_samples_pkey PRIMARY KEY (video_id, captured_at)
  INDEX CREATE INDEX idx_ylvs_captured_at_brin ON public.youtube_live_viewer_samples USING brin (captured_at)
  INDEX CREATE INDEX idx_ylvs_channel_time ON public.youtube_live_viewer_samples USING btree (channel_id, captured_at DESC)

TABLE youtube_milestone_approaching
  COLUMN id integer NOT NULL DEFAULT nextval('youtube_milestone_approaching_id_seq'::regclass)
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN milestone_value bigint NOT NULL
  COLUMN notified_at timestamp with time zone NOT NULL DEFAULT now()
  COLUMN current_subs bigint NOT NULL
  COLUMN chat_notified boolean NOT NULL DEFAULT false
  CONSTRAINT youtube_milestone_approaching_pkey PRIMARY KEY (id)
  CONSTRAINT youtube_milestone_approaching_unique UNIQUE (channel_id, milestone_value)
  INDEX CREATE INDEX idx_approaching_unnotified ON public.youtube_milestone_approaching USING btree (chat_notified) WHERE (chat_notified = false)

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
  INDEX CREATE INDEX idx_milestones_channel_type ON public.youtube_milestones USING btree (channel_id, type)
  INDEX CREATE INDEX idx_milestones_unnotified_achieved_at ON public.youtube_milestones USING btree (achieved_at DESC) WHERE (notified = false)

TABLE youtube_notification_delivery
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
  INDEX CREATE INDEX idx_ynd_sent_cleanup ON public.youtube_notification_delivery USING btree (COALESCE(sent_at, created_at)) WHERE (status = ANY (ARRAY[('SENT'::character varying)::text, ('FAILED'::character varying)::text]))

TABLE youtube_notification_delivery_telemetry
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
  INDEX CREATE INDEX idx_ydt_channel_path_event ON public.youtube_notification_delivery_telemetry USING btree (channel_id, delivery_path, event_at)
  INDEX CREATE UNIQUE INDEX idx_ydt_delivery_attempt ON public.youtube_notification_delivery_telemetry USING btree (delivery_id, attempt_ordinal)
  INDEX CREATE INDEX idx_ydt_logged_event_retention ON public.youtube_notification_delivery_telemetry USING btree (event_at, id) WHERE (logged_at IS NOT NULL)
  INDEX CREATE INDEX idx_ydt_outbox ON public.youtube_notification_delivery_telemetry USING btree (outbox_id)
  INDEX CREATE INDEX idx_ydt_pending_next ON public.youtube_notification_delivery_telemetry USING btree (next_attempt_at, event_at) WHERE (logged_at IS NULL)
  INDEX CREATE INDEX idx_ydt_post_event ON public.youtube_notification_delivery_telemetry USING btree (post_id, event_at)

TABLE youtube_notification_outbox
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
  INDEX CREATE INDEX idx_yno_sent_cleanup ON public.youtube_notification_outbox USING btree (sent_at) WHERE (status = 'SENT'::text)
  INDEX CREATE INDEX idx_yno_status_created ON public.youtube_notification_outbox USING btree (status, created_at)

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
  INDEX CREATE INDEX idx_changes_channel_detected ON public.youtube_stats_changes USING btree (channel_id, detected_at)
  INDEX CREATE INDEX idx_changes_detected ON public.youtube_stats_changes USING btree (detected_at DESC)
  INDEX CREATE INDEX idx_changes_unnotified_detected_at ON public.youtube_stats_changes USING btree (detected_at DESC) WHERE (notified = false)

TABLE youtube_stats_history
  COLUMN time timestamp with time zone NOT NULL
  COLUMN channel_id character varying(64) NOT NULL
  COLUMN member_name text
  COLUMN subscribers bigint
  COLUMN videos bigint
  COLUMN views bigint
  CONSTRAINT youtube_stats_history_pkey PRIMARY KEY ("time", channel_id)
  INDEX CREATE INDEX idx_youtube_stats_history_channel_time ON public.youtube_stats_history USING btree (channel_id, "time" DESC)
  INDEX CREATE INDEX idx_ysh_time_brin ON public.youtube_stats_history USING brin ("time")

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
  INDEX CREATE INDEX idx_yss_channel_ended ON public.youtube_stream_stats USING btree (channel_id, ended_at DESC)

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
