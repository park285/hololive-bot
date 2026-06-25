-- hololive_msa_hot_path_observability.sql
-- Hololive MSA hot-path observability queries.
-- Run manually from psql. This file is read-only except for optional pg_stat_statements reset comments.

-- 1. Top DB time consumers across YouTube producer, delivery, tracking, and alarm dispatch.
SELECT
    calls,
    round(total_exec_time::numeric, 2) AS total_ms,
    round(mean_exec_time::numeric, 3) AS mean_ms,
    round(max_exec_time::numeric, 3) AS max_ms,
    rows,
    shared_blks_hit,
    shared_blks_read,
    temp_blks_read,
    temp_blks_written,
    left(regexp_replace(query, '\s+', ' ', 'g'), 260) AS query
FROM pg_stat_statements
WHERE query ILIKE '%youtube_%'
   OR query ILIKE '%alarm_dispatch%'
ORDER BY total_exec_time DESC
LIMIT 40;

-- 2. Claim and state-transition queries with high tail latency.
SELECT
    calls,
    round(mean_exec_time::numeric, 3) AS mean_ms,
    round(max_exec_time::numeric, 3) AS max_ms,
    left(regexp_replace(query, '\s+', ' ', 'g'), 260) AS query
FROM pg_stat_statements
WHERE (
        query ILIKE '%FOR UPDATE SKIP LOCKED%'
     OR query ILIKE '%authorized_at%'
     OR query ILIKE '%locked_at%'
     OR query ILIKE '%ON CONFLICT%'
    )
  AND (
        query ILIKE '%youtube_%'
     OR query ILIKE '%alarm_dispatch%'
    )
ORDER BY max_exec_time DESC
LIMIT 40;

-- 3. Hot table dead tuple pressure.
SELECT
    relname,
    pg_size_pretty(pg_total_relation_size(relid)) AS total_size,
    n_live_tup,
    n_dead_tup,
    round(n_dead_tup::numeric / greatest(n_live_tup, 1), 4) AS dead_live_ratio,
    last_autovacuum,
    last_autoanalyze
FROM pg_stat_user_tables
WHERE relname IN (
    'youtube_notification_outbox',
    'youtube_notification_delivery',
    'youtube_notification_delivery_telemetry',
    'youtube_content_alarm_tracking',
    'youtube_community_shorts_alarm_states',
    'alarm_dispatch_deliveries',
    'alarm_dispatch_events'
)
ORDER BY dead_live_ratio DESC, n_dead_tup DESC;

-- 4. YouTube notification delivery backlog by status.
SELECT
    status,
    count(*) AS rows,
    min(next_attempt_at) AS oldest_next_attempt_at,
    min(created_at) AS oldest_created_at,
    max(created_at) AS newest_created_at
FROM youtube_notification_delivery
GROUP BY status
ORDER BY rows DESC;

-- 5. YouTube notification outbox backlog by status and kind.
SELECT
    kind,
    status,
    count(*) AS rows,
    min(next_attempt_at) AS oldest_next_attempt_at,
    min(created_at) AS oldest_created_at,
    max(created_at) AS newest_created_at
FROM youtube_notification_outbox
GROUP BY kind, status
ORDER BY rows DESC;

-- 6. Community/shorts stuck claims.
SELECT
    kind,
    count(*) AS stuck_claims,
    min(authorized_at) AS oldest_authorized_at,
    max(authorized_at) AS newest_authorized_at
FROM youtube_community_shorts_alarm_states
WHERE authorized_at IS NOT NULL
  AND alarm_sent_at IS NULL
  AND authorized_at < now() - interval '5 minutes'
GROUP BY kind
ORDER BY stuck_claims DESC;

-- 7. Community/shorts sent-state sanity. Expected: zero rows.
SELECT
    kind,
    post_id,
    count(*) AS rows,
    count(*) FILTER (WHERE alarm_sent_at IS NOT NULL) AS sent_rows
FROM youtube_community_shorts_alarm_states
GROUP BY kind, post_id
HAVING count(*) > 1
    OR count(*) FILTER (WHERE alarm_sent_at IS NOT NULL) > 1;

-- 8. Tracking rows marked sent without matching alarm state. Expected: zero or explainable legacy repair candidates.
SELECT
    t.kind,
    count(*) AS sent_tracking_without_state
FROM youtube_content_alarm_tracking AS t
LEFT JOIN youtube_community_shorts_alarm_states AS s
  ON s.kind = t.kind
 AND s.post_id = t.canonical_content_id
WHERE t.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
  AND t.alarm_sent_at IS NOT NULL
  AND s.post_id IS NULL
GROUP BY t.kind
ORDER BY sent_tracking_without_state DESC;

-- 9. Alarm dispatch backlog by status.
SELECT
    status,
    count(*) AS rows,
    min(created_at) AS oldest_created_at,
    max(created_at) AS newest_created_at
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY rows DESC;

-- 10. Alarm dispatch terminal pressure.
SELECT
    status,
    count(*) AS terminal_rows,
    min(coalesce(sent_at, updated_at, created_at)) AS oldest_terminal_at,
    max(coalesce(sent_at, updated_at, created_at)) AS newest_terminal_at
FROM alarm_dispatch_deliveries
WHERE status IN ('SENT', 'DLQ', 'CANCELLED', 'QUARANTINED')
GROUP BY status
ORDER BY terminal_rows DESC;

-- 11. Connection budget by service/application name.
SELECT
    usename,
    application_name,
    state,
    count(*) AS connections
FROM pg_stat_activity
WHERE datname = current_database()
GROUP BY usename, application_name, state
ORDER BY connections DESC;
