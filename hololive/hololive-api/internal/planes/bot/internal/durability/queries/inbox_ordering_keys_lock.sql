SELECT pg_advisory_xact_lock(hashtextextended(ordered.ordering_key, 0))
FROM (
    SELECT DISTINCT ordering_key
    FROM unnest($1::text[]) AS input(ordering_key)
    ORDER BY ordering_key
) AS ordered
