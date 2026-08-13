SELECT pg_advisory_xact_lock(
    hashtextextended($1::text || E'\x1f' || $2::text, 0)
)
