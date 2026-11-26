UPDATE schedule_poster_notices n
SET lease_until = now() + make_interval(secs => $1)
WHERE n.id = (
    SELECT id
    FROM schedule_poster_notices
    WHERE status = 'pending' AND (lease_until IS NULL OR lease_until < now())
    ORDER BY id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING n.id, n.kind, n.poster_id, n.channel_id, COALESCE(n.post_id, 0), n.created_at, now()
