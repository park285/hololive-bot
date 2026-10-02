SELECT pg_advisory_xact_lock(
    hashtextextended(
        concat_ws(E'\\x1f', 'content_subject_head', $1::text, $2::text),
        0
    )
)
