SELECT pg_advisory_xact_lock(
    hashtextextended(
        concat_ws(E'\\x1f', 'live_subject_head', $1::text),
        0
    )
)
