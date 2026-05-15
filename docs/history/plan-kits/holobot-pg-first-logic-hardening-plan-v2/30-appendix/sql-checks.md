# Appendix. SQL Checks

## Status count

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
```

## Active backlog

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry', 'leased', 'sending')
GROUP BY status
ORDER BY status;
```

## Oldest pending age

```sql
SELECT COALESCE(EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at)), 0) AS oldest_pending_age_seconds
FROM alarm_dispatch_deliveries
WHERE status = 'pending'
  AND next_attempt_at <= NOW();
```

## Oldest retry age

```sql
SELECT COALESCE(EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at)), 0) AS oldest_retry_age_seconds
FROM alarm_dispatch_deliveries
WHERE status = 'retry'
  AND next_attempt_at <= NOW();
```

## Oldest sending age

```sql
SELECT COALESCE(EXTRACT(EPOCH FROM NOW() - MIN(sending_started_at)), 0) AS oldest_sending_age_seconds
FROM alarm_dispatch_deliveries
WHERE status = 'sending';
```

## Stale leased candidates

```sql
SELECT id, locked_by, locked_at, lock_expires_at, last_error
FROM alarm_dispatch_deliveries
WHERE status = 'leased'
  AND lock_expires_at < NOW()
ORDER BY lock_expires_at ASC, id ASC
LIMIT 50;
```

## Stale sending candidates

```sql
SELECT id, locked_by, sending_started_at, last_error
FROM alarm_dispatch_deliveries
WHERE status = 'sending'
  AND sending_started_at < NOW() - INTERVAL '60 seconds'
ORDER BY sending_started_at ASC, id ASC
LIMIT 50;
```

## Duplicate and legacy key accounting

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
WHERE dedupe_key NOT LIKE 'v2:%'
GROUP BY status
ORDER BY status;
```

## Retention dry-run

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
WHERE (
    status = 'sent' AND sent_at < NOW() - INTERVAL '90 days'
) OR (
    status = 'dlq' AND dlq_at < NOW() - INTERVAL '180 days'
) OR (
    status = 'quarantined' AND quarantined_at < NOW() - INTERVAL '180 days'
) OR (
    status = 'cancelled' AND cancelled_at < NOW() - INTERVAL '90 days'
)
GROUP BY status
ORDER BY status;
```

## Orphan events dry-run

```sql
SELECT count(*)
FROM alarm_dispatch_events e
WHERE e.created_at < NOW() - INTERVAL '90 days'
  AND NOT EXISTS (
      SELECT 1
      FROM alarm_dispatch_deliveries d
      WHERE d.event_id = e.id
  );
```

## Index check

```sql
SELECT indexname, indexdef
FROM pg_indexes
WHERE tablename IN ('alarm_dispatch_events', 'alarm_dispatch_deliveries')
ORDER BY tablename, indexname;
```
