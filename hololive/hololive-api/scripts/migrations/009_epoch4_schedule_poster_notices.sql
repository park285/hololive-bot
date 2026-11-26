BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE TABLE public.schedule_poster_notices (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('published', 'changed')),
    poster_id bigint NOT NULL REFERENCES public.schedule_posters (id) ON DELETE CASCADE,
    channel_id character varying(64) NOT NULL CHECK (length(channel_id) > 0),
    post_id bigint CHECK (post_id > 0),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'skipped', 'baseline')),
    rooms integer CHECK (rooms >= 0),
    lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    settled_at timestamptz,
    CHECK ((kind = 'changed') = (post_id IS NOT NULL)),
    CHECK ((status = 'pending') = (settled_at IS NULL)),
    CHECK (status = 'sent' OR rooms IS NULL)
);

CREATE UNIQUE INDEX schedule_poster_notices_published_key ON public.schedule_poster_notices (poster_id) WHERE kind = 'published' AND status <> 'skipped';
CREATE UNIQUE INDEX schedule_poster_notices_changed_key ON public.schedule_poster_notices (channel_id, post_id) WHERE kind = 'changed' AND status <> 'skipped';
CREATE INDEX schedule_poster_notices_pending ON public.schedule_poster_notices (id) WHERE status = 'pending';

INSERT INTO public.schedule_poster_notices (kind, poster_id, channel_id, status, settled_at)
SELECT 'published', p.id, p.channel_id, 'baseline', now()
FROM public.schedule_posters p
WHERE p.rows_extracted_at IS NOT NULL;

INSERT INTO public.schedule_poster_notices (kind, poster_id, channel_id, post_id, status, settled_at)
SELECT 'changed', d.matched_poster_id, d.channel_id, d.post_id, 'baseline', now()
FROM public.schedule_poster_posts d
JOIN public.schedule_posters p ON p.id = d.matched_poster_id
WHERE d.status = 'matched';

REVOKE ALL ON TABLE public.schedule_poster_notices FROM PUBLIC;

DO $acl$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT DISTINCT r.rolname
        FROM pg_catalog.pg_class c
        CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
        JOIN pg_catalog.pg_roles r ON r.oid = a.grantee
        WHERE c.oid = 'public.schedule_poster_notices'::regclass AND a.grantee <> c.relowner
    LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.schedule_poster_notices FROM %I', item.rolname);
    END LOOP;
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT, INSERT, UPDATE ON TABLE public.schedule_poster_notices TO hololive_runtime;
    END IF;
END
$acl$;

COMMIT;
