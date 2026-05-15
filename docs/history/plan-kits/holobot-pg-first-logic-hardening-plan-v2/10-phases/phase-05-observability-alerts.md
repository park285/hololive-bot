# Phase 05. Observability and Alerts

## 목적

PG-first 운영에서 “빨라 보이지만 실제로 늦어지는” 상태를 놓치지 않도록 metric과 alert를 강화합니다.

## 기존 metric 활용

이미 존재하는 metric:

```text
alarm_dispatch_publish_batch_duration_seconds
alarm_dispatch_publish_requested_deliveries_total
alarm_dispatch_publish_processed_deliveries_total
alarm_dispatch_publish_inserted_deliveries_total
alarm_dispatch_publish_duplicate_deliveries_total
alarm_dispatch_publish_hash_conflict_total
alarm_dispatch_wakeup_sent_total
alarm_dispatch_wakeup_suppressed_total
alarm_dispatch_wakeup_failed_total
alarm_dispatch_pg_claimed_total
alarm_dispatch_pg_mark_sending_failed_total
alarm_dispatch_pg_mark_sent_failed_total
alarm_dispatch_pg_quarantined_total
alarm_dispatch_pg_dlq_total
alarm_dispatch_pg_retry_scheduled_total
alarm_dispatch_recovery_failed_total
alarm_dispatch_recovery_rows_total
```

## 추가 권장 metric

```text
alarm_dispatch_runner_empty_polls_total{consumer_mode}
alarm_dispatch_runner_idle_wait_seconds{consumer_mode, wait_mode}
alarm_dispatch_runner_wakeup_consumed_total
alarm_dispatch_runner_wakeup_timeout_total
alarm_dispatch_runner_wakeup_error_total
alarm_dispatch_runner_post_send_quarantined_total
alarm_dispatch_runner_mark_dispatched_after_send_failed_total
alarm_dispatch_pg_backlog_rows{status}
alarm_dispatch_pg_oldest_pending_age_seconds
alarm_dispatch_pg_oldest_retry_age_seconds
alarm_dispatch_pg_oldest_sending_age_seconds
alarm_dispatch_pg_retention_deleted_rows_total{status}
alarm_dispatch_pg_retention_failed_total
```

## Backlog probe SQL

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry', 'leased', 'sending')
GROUP BY status;
```

```sql
SELECT EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at))
FROM alarm_dispatch_deliveries
WHERE status = 'pending'
  AND next_attempt_at <= NOW();
```

```sql
SELECT EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at))
FROM alarm_dispatch_deliveries
WHERE status = 'retry'
  AND next_attempt_at <= NOW();
```

```sql
SELECT EXTRACT(EPOCH FROM NOW() - MIN(sending_started_at))
FROM alarm_dispatch_deliveries
WHERE status = 'sending';
```

## Alert rules

### Pending backlog

```yaml
- alert: AlarmDispatchPendingBacklog
  expr: alarm_dispatch_pg_oldest_pending_age_seconds > 60
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Alarm dispatch pending backlog is aging"
```

### Critical pending backlog

```yaml
- alert: AlarmDispatchPendingBacklogCritical
  expr: alarm_dispatch_pg_oldest_pending_age_seconds > 180
  for: 3m
  labels:
    severity: critical
  annotations:
    summary: "Alarm dispatch pending backlog is critically old"
```

### Quarantine spike

```yaml
- alert: AlarmDispatchQuarantineSpike
  expr: increase(alarm_dispatch_pg_quarantined_total[10m]) > 0
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "Alarm dispatch rows entered quarantine"
```

### Mark sent failure

```yaml
- alert: AlarmDispatchMarkSentFailure
  expr: increase(alarm_dispatch_pg_mark_sent_failed_total[5m]) > 0
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "Alarm dispatch mark-sent failed after external send"
```

### Hash conflict

```yaml
- alert: AlarmDispatchHashConflict
  expr: increase(alarm_dispatch_publish_hash_conflict_total[10m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Alarm dispatch event hash conflict detected"
```

## Dashboard panels

- Publish duration p50/p95/p99.
- Requested vs inserted vs duplicate deliveries.
- Wakeup sent/suppressed/failed.
- Active status counts: pending/retry/leased/sending.
- Terminal status counts: sent/dlq/quarantined/cancelled.
- Oldest pending/retry/sending age.
- Quarantine and DLQ rate.
- Recovery touched rows.
- Retention deleted rows.
- DB pool usage if available.

## 완료 기준

- backlog age가 dashboard에 보입니다.
- quarantine이 alert로 연결됩니다.
- wakeup failure가 alert로 연결됩니다.
- mark sent failure가 critical입니다.
- hash conflict가 critical입니다.
