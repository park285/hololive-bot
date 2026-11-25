BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE TABLE public.kakao_room_delivery_pauses (
    room_id character varying(100) PRIMARY KEY CHECK (length(room_id) > 0),
    reason text NOT NULL CHECK (reason = 'room_not_joined'),
    paused_at timestamptz NOT NULL
);

REVOKE ALL ON TABLE public.kakao_room_delivery_pauses FROM PUBLIC;

DO $acl$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT DISTINCT r.rolname
        FROM pg_catalog.pg_class c
        CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
        JOIN pg_catalog.pg_roles r ON r.oid = a.grantee
        WHERE c.oid = 'public.kakao_room_delivery_pauses'::regclass AND a.grantee <> c.relowner
    LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.kakao_room_delivery_pauses FROM %I', item.rolname);
    END LOOP;
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_alarm_worker') THEN
        GRANT SELECT, INSERT ON TABLE public.kakao_room_delivery_pauses TO hololive_alarm_worker;
    END IF;
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT, DELETE ON TABLE public.kakao_room_delivery_pauses TO hololive_runtime;
    END IF;
END
$acl$;

COMMIT;
