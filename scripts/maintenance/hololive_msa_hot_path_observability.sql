-- hololive_msa_hot_path_observability.sql
-- This file is not a migration. Run manually against a read-only session when
-- checking hot-path and alarm dispatch health.

\echo 'alarm_dispatch_deliveries terminal rows'
SELECT
    status,
    count(*) AS terminal_rows,
    min(coalesce(sent_at, dlq_at, cancelled_at, quarantined_at, updated_at, created_at)) AS oldest_terminal_at,
    max(coalesce(sent_at, dlq_at, cancelled_at, quarantined_at, updated_at, created_at)) AS newest_terminal_at
FROM alarm_dispatch_deliveries
WHERE status IN ('sent', 'dlq', 'cancelled', 'quarantined')
GROUP BY status
ORDER BY terminal_rows DESC;

\echo 'alarm dispatch active backlog'
SELECT
    status,
    count(*) AS rows,
    min(next_attempt_at) AS oldest_next_attempt_at,
    max(updated_at) AS newest_updated_at
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry', 'leased', 'sending')
GROUP BY status
ORDER BY rows DESC;

\echo 'community/shorts stuck claim states'
SELECT
    kind,
    delivery_status,
    count(*) AS rows,
    min(authorized_at) AS oldest_authorized_at,
    max(updated_at) AS newest_updated_at
FROM youtube_community_shorts_alarm_states
WHERE authorized_at IS NOT NULL
  AND alarm_sent_at IS NULL
GROUP BY kind, delivery_status
ORDER BY rows DESC;

\echo 'community/shorts duplicate sent-state candidates'
SELECT
    kind,
    post_id,
    count(*) AS rows,
    min(alarm_sent_at) AS first_alarm_sent_at,
    max(alarm_sent_at) AS last_alarm_sent_at
FROM youtube_community_shorts_alarm_states
WHERE alarm_sent_at IS NOT NULL
GROUP BY kind, post_id
HAVING count(*) > 1
ORDER BY rows DESC, last_alarm_sent_at DESC
LIMIT 50;

\echo 'sent tracking rows missing canonical alarm state'
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

\echo 'pg_stat_statements youtube/alarm hot queries'
SELECT
    calls,
    round(mean_exec_time::numeric, 3) AS mean_ms,
    round(max_exec_time::numeric, 3) AS max_ms,
    rows,
    left(regexp_replace(query, '\s+', ' ', 'g'), 240) AS query
FROM pg_stat_statements
WHERE query ILIKE '%youtube_notification_delivery%'
   OR query ILIKE '%youtube_notification_outbox%'
   OR query ILIKE '%youtube_content_alarm_tracking%'
   OR query ILIKE '%youtube_community_shorts_alarm_states%'
   OR query ILIKE '%alarm_dispatch_deliveries%'
ORDER BY total_exec_time DESC
LIMIT 30;
