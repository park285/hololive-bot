DO $boundary$
DECLARE
    constraints_to_check text;
    initially_deferred text;
    ambiguous text;
BEGIN
    WITH names AS (
        SELECT pg_catalog.format('%I.%I', n.nspname, c.conname) AS qualified,
               bool_or(c.condeferred) AS any_deferred,
               bool_and(c.condeferrable AND c.condeferred) AS all_deferred
        FROM pg_catalog.pg_constraint c
        JOIN pg_catalog.pg_namespace n ON n.oid = c.connamespace
        WHERE NOT pg_catalog.pg_is_other_temp_schema(n.oid)
        GROUP BY n.nspname, c.conname
        HAVING bool_or(c.condeferrable)
    )
    SELECT string_agg(qualified, ', ' ORDER BY qualified),
           string_agg(qualified, ', ' ORDER BY qualified) FILTER (WHERE all_deferred),
           min(qualified) FILTER (WHERE any_deferred AND NOT all_deferred)
    INTO constraints_to_check, initially_deferred, ambiguous
    FROM names;

    IF ambiguous IS NOT NULL THEN
        RAISE EXCEPTION 'conflicting initial timing for constraints named %', ambiguous;
    END IF;
    IF constraints_to_check IS NOT NULL THEN
        EXECUTE 'SET CONSTRAINTS ' || constraints_to_check || ' IMMEDIATE';
    END IF;
    IF initially_deferred IS NOT NULL THEN
        EXECUTE 'SET CONSTRAINTS ' || initially_deferred || ' DEFERRED';
    END IF;
END
$boundary$;
