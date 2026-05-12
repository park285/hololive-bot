# T20. Metrics catalog implementation

## 목적

전환 중 병목과 correctness 이상을 metric으로 볼 수 있게 합니다.

## 필수 metric

```text
alarm_dispatch_publish_batch_duration_seconds
alarm_dispatch_publish_requested_deliveries_total
alarm_dispatch_publish_inserted_deliveries_total
alarm_dispatch_publish_duplicate_deliveries_total
alarm_dispatch_publish_hash_conflict_total
alarm_dispatch_wakeup_sent_total
alarm_dispatch_wakeup_suppressed_total
alarm_dispatch_wakeup_failed_total
alarm_dispatch_claimed_total
alarm_dispatch_mark_sending_failed_total
alarm_dispatch_sent_total
alarm_dispatch_retry_scheduled_total
alarm_dispatch_dlq_total
alarm_dispatch_quarantined_total
alarm_dispatch_recovery_leased_total
alarm_dispatch_recovery_sending_quarantined_total
```

## 완료 기준

- publisher, dispatcher, repository 주요 경로에 metric이 있습니다.
- cardinality 높은 label을 쓰지 않습니다.
- room_id, stream_id를 metric label로 쓰지 않습니다.

## LLM 프롬프트

알람 dispatch metric을 추가하십시오. label cardinality를 낮게 유지하고, room_id/stream_id는 label로 쓰지 마십시오.
