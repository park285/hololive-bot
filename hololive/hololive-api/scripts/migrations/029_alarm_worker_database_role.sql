ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM PUBLIC;

REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM PUBLIC;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM PUBLIC;
REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM PUBLIC;

DO $acl$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM hololive_runtime;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM hololive_runtime;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM hololive_runtime;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_alarm_worker') THEN
        REVOKE ALL ON SCHEMA public FROM hololive_alarm_worker;
        REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM hololive_alarm_worker;
        REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM hololive_alarm_worker;
        REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM hololive_alarm_worker;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM hololive_alarm_worker;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM hololive_alarm_worker;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM hololive_alarm_worker;

        GRANT USAGE ON SCHEMA public TO hololive_alarm_worker;

        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE
            public.alarms,
            public.alarm_room_display_names,
            public.alarm_dispatch_deliveries,
            public.alarm_dispatch_send_units,
            public.alarm_upcoming_candidates,
            public.youtube_notification_delivery,
            public.youtube_notification_send_request,
            public.youtube_notification_delivery_ledger,
            public.youtube_notification_delivery_telemetry,
            public.x_space_starts
        TO hololive_alarm_worker;

        GRANT SELECT, INSERT, DELETE ON TABLE
            public.alarm_dispatch_events
        TO hololive_alarm_worker;

        GRANT SELECT, INSERT, UPDATE ON TABLE
            public.alarm_dispatch_event_collisions,
            public.kakao_rooms,
            public.youtube_community_shorts_alarm_states
        TO hololive_alarm_worker;

        GRANT SELECT, UPDATE, DELETE ON TABLE
            public.youtube_notification_outbox
        TO hololive_alarm_worker;

        GRANT SELECT, UPDATE ON TABLE
            public.x_space_session,
            public.youtube_content_alarm_tracking
        TO hololive_alarm_worker;

        GRANT SELECT ON TABLE
            public.alarm_dispatch_closeout_receipts,
            public.members,
            public.message_strings,
            public.notification_templates,
            public.youtube_live_sessions,
            public.youtube_schedule_items
        TO hololive_alarm_worker;

        GRANT USAGE ON SEQUENCE
            public.alarms_id_seq,
            public.alarm_dispatch_deliveries_id_seq,
            public.alarm_dispatch_event_collisions_id_seq,
            public.alarm_dispatch_events_id_seq,
            public.alarm_dispatch_send_units_id_seq,
            public.youtube_notification_delivery_id_seq,
            public.youtube_notification_delivery_telemetry_id_seq
        TO hololive_alarm_worker;
    END IF;
END
$acl$;
