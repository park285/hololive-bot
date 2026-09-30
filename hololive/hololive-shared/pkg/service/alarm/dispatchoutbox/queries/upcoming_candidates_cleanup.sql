WITH picked AS (
    SELECT dedupe_key FROM alarm_upcoming_candidates
    WHERE terminal_at < NOW() - make_interval(days => $1)
    ORDER BY terminal_at, dedupe_key LIMIT $2
    FOR UPDATE SKIP LOCKED
)
DELETE FROM alarm_upcoming_candidates c USING picked p
WHERE c.dedupe_key = p.dedupe_key AND c.outcome <> 'pending'
    AND c.terminal_at < NOW() - make_interval(days => $1)
