-- alarm-worker 전용 로그인(hololive_alarm_worker)은 worker 코드가 쓰는 표와 sequence만 가진다. 이 역할에는 미래 객체 기본 권한이 없으므로
-- worker가 새 표를 써야 하면 그 migration이 직접 GRANT한다. 역할이 없는 환경(새 dbtest template 등)에서는 건너뛴다.
-- 일괄 REVOKE는 쓰지 않는다. public의 확장 함수는 superuser 소유라 migrator가 REVOKE하면 42501로 실패한다.
BEGIN;
SET LOCAL lock_timeout = '3s';

DO $acl$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_alarm_worker') THEN
        RAISE NOTICE 'hololive_alarm_worker role is absent; skipping alarm worker grants';
        RETURN;
    END IF;

    GRANT USAGE ON SCHEMA public TO hololive_alarm_worker;

    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE
        public.alarms,
        public.alarm_room_display_names,
        public.alarm_dispatch_deliveries,
        public.alarm_dispatch_events,
        public.alarm_dispatch_send_units,
        public.alarm_upcoming_candidates,
        public.youtube_notification_send_request,
        public.youtube_notification_delivery_ledger,
        public.youtube_notification_delivery_telemetry
    TO hololive_alarm_worker;

    GRANT SELECT, INSERT, UPDATE ON TABLE
        public.alarm_dispatch_event_collisions,
        public.kakao_rooms,
        public.youtube_community_shorts_alarm_states,
        public.youtube_notification_delivery
    TO hololive_alarm_worker;

    GRANT SELECT, INSERT, DELETE ON TABLE
        public.x_space_starts
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
END
$acl$;

COMMIT;
