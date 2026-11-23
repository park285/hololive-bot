BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE TABLE public.bot_webhook_nonces (
    digest bytea PRIMARY KEY CHECK (octet_length(digest) = 32),
    expires_at timestamptz NOT NULL
);

CREATE INDEX bot_webhook_nonces_expiry ON public.bot_webhook_nonces (expires_at);

CREATE FUNCTION public.reserve_bot_webhook_nonce(p_digest bytea, p_ttl_us bigint) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    checked_at timestamptz := clock_timestamp();
    reserved boolean;
BEGIN
    IF p_ttl_us IS DISTINCT FROM 600000000 THEN
        RAISE EXCEPTION 'unsupported webhook nonce replay window' USING ERRCODE = '22023';
    END IF;

    INSERT INTO public.bot_webhook_nonces AS nonce (digest, expires_at)
    VALUES (p_digest, checked_at + p_ttl_us * interval '1 microsecond')
    ON CONFLICT (digest) DO UPDATE SET expires_at = EXCLUDED.expires_at
    WHERE nonce.expires_at <= checked_at;
    reserved := FOUND;

    DELETE FROM public.bot_webhook_nonces AS nonce
    USING (SELECT digest FROM public.bot_webhook_nonces
           WHERE expires_at <= checked_at ORDER BY expires_at LIMIT 64 FOR UPDATE SKIP LOCKED) AS expired
    WHERE nonce.digest = expired.digest;

    RETURN reserved;
END
$$;

REVOKE ALL ON TABLE public.bot_webhook_nonces FROM PUBLIC;
REVOKE ALL ON FUNCTION public.reserve_bot_webhook_nonce(bytea,bigint) FROM PUBLIC;

DO $acl$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT DISTINCT r.rolname, 'TABLE public.bot_webhook_nonces' AS target
        FROM pg_catalog.pg_class c
        CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
        JOIN pg_catalog.pg_roles r ON r.oid = a.grantee
        WHERE c.oid = 'public.bot_webhook_nonces'::regclass AND a.grantee <> c.relowner
        UNION
        SELECT DISTINCT r.rolname, 'FUNCTION public.reserve_bot_webhook_nonce(bytea,bigint)'
        FROM pg_catalog.pg_proc p
        CROSS JOIN LATERAL pg_catalog.aclexplode(p.proacl) a
        JOIN pg_catalog.pg_roles r ON r.oid = a.grantee
        WHERE p.oid = 'public.reserve_bot_webhook_nonce(bytea,bigint)'::regprocedure AND a.grantee <> p.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON %s FROM %I', item.target, item.rolname);
    END LOOP;
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.reserve_bot_webhook_nonce(bytea,bigint) TO hololive_runtime;
    END IF;
END
$acl$;

COMMIT;
