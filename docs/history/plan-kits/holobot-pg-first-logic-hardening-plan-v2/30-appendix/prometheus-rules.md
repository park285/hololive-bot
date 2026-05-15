# Appendix. Prometheus Alert Rules

```yaml
groups:
  - name: holobot-alarm-dispatch-pg-first
    rules:
      - alert: AlarmDispatchPendingBacklog
        expr: alarm_dispatch_pg_oldest_pending_age_seconds > 60
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Alarm dispatch pending backlog is aging"
          description: "Oldest pending dispatch row is older than 60 seconds."

      - alert: AlarmDispatchPendingBacklogCritical
        expr: alarm_dispatch_pg_oldest_pending_age_seconds > 180
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "Alarm dispatch pending backlog is critically old"
          description: "Oldest pending dispatch row is older than 180 seconds."

      - alert: AlarmDispatchRetryBacklog
        expr: alarm_dispatch_pg_oldest_retry_age_seconds > 300
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Alarm dispatch retry backlog is aging"

      - alert: AlarmDispatchStaleSending
        expr: alarm_dispatch_pg_oldest_sending_age_seconds > 120
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Alarm dispatch sending row is stale"

      - alert: AlarmDispatchQuarantineSpike
        expr: increase(alarm_dispatch_pg_quarantined_total[10m]) > 0
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Alarm dispatch rows entered quarantine"

      - alert: AlarmDispatchMarkSentFailure
        expr: increase(alarm_dispatch_pg_mark_sent_failed_total[5m]) > 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Alarm dispatch mark-sent failed after external send"

      - alert: AlarmDispatchWakeupFailed
        expr: increase(alarm_dispatch_wakeup_failed_total[5m]) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Alarm dispatch wakeup failed"

      - alert: AlarmDispatchHashConflict
        expr: increase(alarm_dispatch_publish_hash_conflict_total[10m]) > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Alarm dispatch event hash conflict detected"

      - alert: AlarmDispatchRetentionFailed
        expr: increase(alarm_dispatch_pg_retention_failed_total[1h]) > 0
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: "Alarm dispatch retention failed"
```
