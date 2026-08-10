SELECT EXISTS (
    SELECT 1
    FROM schema_migrations
    WHERE filename <> ALL($1::text[])
)
