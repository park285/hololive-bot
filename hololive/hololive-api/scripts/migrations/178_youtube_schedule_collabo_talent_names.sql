BEGIN;

ALTER TABLE youtube_schedule_items
    ADD COLUMN IF NOT EXISTS collabo_talent_names TEXT[] NOT NULL DEFAULT '{}';

CREATE OR REPLACE FUNCTION public.youtube_schedule_collabo_talent_names_valid(names TEXT[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
STRICT
SET search_path = pg_catalog
AS $$
    SELECT COALESCE(pg_catalog.array_ndims(names), 1) = 1
       AND COALESCE(pg_catalog.array_lower(names, 1), 1) = 1
       AND pg_catalog.cardinality(names) <= 32
       AND NOT EXISTS (
           SELECT 1
           FROM pg_catalog.unnest(names) AS name
           WHERE name IS NULL
              OR pg_catalog.octet_length(name) < 1
              OR pg_catalog.octet_length(name) > 256
       );
$$;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.youtube_schedule_items'::regclass
          AND conname = 'chk_youtube_schedule_item_collabo_talent_names'
    ) THEN
        ALTER TABLE youtube_schedule_items
            ADD CONSTRAINT chk_youtube_schedule_item_collabo_talent_names
            CHECK (public.youtube_schedule_collabo_talent_names_valid(collabo_talent_names))
            NOT VALID;
    END IF;
END
$migration$;

COMMIT;

ALTER TABLE youtube_schedule_items
    VALIDATE CONSTRAINT chk_youtube_schedule_item_collabo_talent_names;

BEGIN;

REVOKE ALL ON FUNCTION public.youtube_schedule_collabo_talent_names_valid(TEXT[]) FROM PUBLIC;

DO $migration$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.youtube_schedule_collabo_talent_names_valid(TEXT[]) TO hololive_runtime;
    END IF;
END
$migration$;

COMMENT ON COLUMN youtube_schedule_items.collabo_talent_names IS
    'Official schedule collaboTalents[].name evidence. Empty means solo or no guests. Not used for owner identity.';

COMMIT;
