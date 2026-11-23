DO $bootstrap$
DECLARE
    admin_oid oid;
    schema_oid oid;
    gate_oid oid;
    trigger_oid oid;
BEGIN
    SELECT oid INTO admin_oid FROM pg_catalog.pg_roles WHERE rolname = CURRENT_USER AND rolsuper;
    IF admin_oid IS NULL THEN
        RAISE EXCEPTION 'writer admission bootstrap requires the privileged admin' USING ERRCODE = '42501';
    END IF;

    SELECT oid INTO schema_oid FROM pg_catalog.pg_namespace WHERE nspname = 'hololive_writer_admission';
    IF schema_oid IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_namespace WHERE oid = schema_oid AND nspowner = admin_oid
    ) THEN
        RAISE EXCEPTION 'unexpected writer admission schema owner' USING ERRCODE = '42501';
    END IF;
    IF schema_oid IS NULL THEN
        CREATE SCHEMA hololive_writer_admission;
        SELECT oid INTO schema_oid FROM pg_catalog.pg_namespace WHERE nspname = 'hololive_writer_admission';
    END IF;

    gate_oid := pg_catalog.to_regprocedure('hololive_writer_admission.reject_during_writer_drain()');
    IF gate_oid IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_proc WHERE oid = gate_oid AND proowner = admin_oid
    ) THEN
        RAISE EXCEPTION 'unexpected writer admission function owner' USING ERRCODE = '42501';
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_catalog.pg_depend AS d
        WHERE d.refclassid = 'pg_catalog.pg_namespace'::pg_catalog.regclass AND d.refobjid = schema_oid
          AND NOT (d.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass AND d.objid = COALESCE(gate_oid, 0))
    ) THEN
        RAISE EXCEPTION 'unexpected writer admission schema contents' USING ERRCODE = '42501';
    END IF;

    EXECUTE $definition$
        CREATE OR REPLACE FUNCTION hololive_writer_admission.reject_during_writer_drain()
        RETURNS event_trigger
        LANGUAGE plpgsql SECURITY DEFINER
        SET search_path = pg_catalog, pg_temp
        AS $gate$
        BEGIN
            IF SESSION_USER IN (
                'hololive_runtime', 'hololive_alarm_worker',
                'hololive_collector_a', 'hololive_collector_b',
                'hololive_collector_c', 'hololive_collector_d'
            ) AND (EXISTS (
                SELECT FROM pg_catalog.pg_stat_activity AS a
                JOIN pg_catalog.pg_roles AS r ON r.oid = a.usesysid AND r.rolsuper
                WHERE a.datname = pg_catalog.current_database()
                  AND a.application_name IN ('hololive-writers-draining', 'hololive-writers-drained')
            ) OR EXISTS (
                SELECT FROM pg_catalog.pg_stat_activity AS a
                WHERE a.datname = pg_catalog.current_database()
                  AND a.usename = 'hololive_migrator'
                  AND a.application_name = 'hololive-migration-window'
            )) THEN
                RAISE EXCEPTION 'application writers are closed for the card migration window' USING ERRCODE = '57P03';
            END IF;
        END
        $gate$
    $definition$;

    REVOKE ALL ON SCHEMA hololive_writer_admission FROM PUBLIC;
    REVOKE ALL ON FUNCTION hololive_writer_admission.reject_during_writer_drain() FROM PUBLIC;

    SELECT oid INTO trigger_oid FROM pg_catalog.pg_event_trigger WHERE evtname = 'hololive_writer_admission';
    IF trigger_oid IS NULL THEN
        CREATE EVENT TRIGGER hololive_writer_admission ON login
            EXECUTE FUNCTION hololive_writer_admission.reject_during_writer_drain();
    ELSIF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_event_trigger
        WHERE oid = trigger_oid AND evtevent = 'login' AND evtowner = admin_oid
          AND evtfoid = 'hololive_writer_admission.reject_during_writer_drain()'::pg_catalog.regprocedure
          AND evttags IS NULL
    ) THEN
        RAISE EXCEPTION 'unexpected writer admission event trigger' USING ERRCODE = '42501';
    END IF;
    ALTER EVENT TRIGGER hololive_writer_admission ENABLE ALWAYS;
END
$bootstrap$;
