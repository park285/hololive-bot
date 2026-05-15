# T06. Observability

## 목표

PG-first dispatch의 latency, backlog, ambiguous failure, retention 상태를 운영자가 즉시 볼 수 있게 합니다.

## PATH

```text
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner.go
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_idle.go
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_maintenance.go
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/metrics.go
hololive/hololive-shared/pkg/service/alarm/queue/metrics.go
```

## 추가 metric

```text
alarm_dispatch_runner_empty_polls_total
alarm_dispatch_runner_idle_wait_seconds
alarm_dispatch_runner_wakeup_consumed_total
alarm_dispatch_runner_wakeup_timeout_total
alarm_dispatch_runner_post_send_quarantined_total
alarm_dispatch_pg_backlog_rows
alarm_dispatch_pg_oldest_pending_age_seconds
alarm_dispatch_pg_oldest_retry_age_seconds
alarm_dispatch_pg_oldest_sending_age_seconds
alarm_dispatch_pg_retention_deleted_rows_total
```

## 테스트

- metric registration.
- counter increments.
- backlog gauge query.
