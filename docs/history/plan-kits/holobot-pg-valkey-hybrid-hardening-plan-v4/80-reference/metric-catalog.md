# Metric Catalog

## Publisher

```text
alarm_dispatch_publish_batch_duration_seconds{mode}
alarm_dispatch_publish_requested_events_total{mode}
alarm_dispatch_publish_inserted_events_total{mode}
alarm_dispatch_publish_duplicate_events_total{mode}
alarm_dispatch_publish_hash_conflict_total{mode}
alarm_dispatch_publish_requested_deliveries_total{mode}
alarm_dispatch_publish_inserted_deliveries_total{mode}
alarm_dispatch_publish_duplicate_deliveries_total{mode}
alarm_dispatch_publish_batch_size{mode}
```

## Wakeup

```text
alarm_dispatch_wakeup_sent_total
alarm_dispatch_wakeup_suppressed_total
alarm_dispatch_wakeup_failed_total{reason}
alarm_dispatch_wakeup_wait_timeout_total
alarm_dispatch_wakeup_wait_error_total{reason}
```

## Dispatcher

```text
alarm_dispatch_claimed_total
alarm_dispatch_claim_batch_size
alarm_dispatch_load_events_duration_seconds
alarm_dispatch_mark_sending_total
alarm_dispatch_mark_sending_failed_total
alarm_dispatch_sent_total
alarm_dispatch_mark_sent_failed_total
alarm_dispatch_retry_scheduled_total
alarm_dispatch_dlq_total
alarm_dispatch_quarantined_total
```

## Reconciliation

```text
alarm_dispatch_recovery_last_success_timestamp_seconds
alarm_dispatch_recovery_failed_total{type}
alarm_dispatch_recovery_rows_total{type}
```

## 금지 label

- room_id
- stream_id
- channel_id가 cardinality 높으면 금지
- dedupe_key
- event_key
