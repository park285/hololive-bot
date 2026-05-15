# 05. Acceptance Criteria

## Contract acceptance

- `AlarmQueueEnvelope` field와 JSON key가 변경되지 않았습니다.
- `QueueEnvelopeVersionV1` 값이 변경되지 않았습니다.
- 기존 alarm HTTP API path가 변경되지 않았습니다.
- 기존 Valkey queue key가 변경되지 않았습니다.
- 기존 PG table/column/status가 변경되지 않았습니다.
- `shadowed` row를 `pending`으로 자동 승격하는 코드가 없습니다.
- legacy Valkey residue를 PG로 replay하는 코드가 없습니다.

## Correctness acceptance

- `MarkSending` 이전 failure는 retry/DLQ 경로로 처리됩니다.
- `MarkSending` 이후 `SendMessage` failure는 PG consumer path에서 quarantine됩니다.
- `MarkDispatched` failure는 자동 retry를 만들지 않습니다.
- `pending`/`retry`만 claim됩니다.
- `leased` row는 lease 만료 시 retry로 복구됩니다.
- stale `sending` row는 quarantine됩니다.
- `sent`, `dlq`, `quarantined`, `cancelled` row는 dedupe conflict로 `pending`에 복귀하지 않습니다.

## Performance acceptance

- idle 상태에서 alarm-worker 내장 PG consumer가 25ms 고정 polling을 하지 않습니다.
- wakeup enabled 상태에서는 wakeup token 수신 즉시 다음 claim을 시도합니다.
- wakeup unavailable 상태에서도 poll interval 기반으로 due row를 처리합니다.
- maxBatchesPerWake로 연속 batch 독점을 제한합니다.
- batch size와 pool size는 compose에서 조정 가능합니다.

## Operations acceptance

- retention은 chunk 단위로 실행됩니다.
- retention은 단일 실행 보장을 갖습니다.
- retention query timeout이 있습니다.
- rollback runbook은 stranded PG row와 legacy Valkey residue를 분리해서 다룹니다.
- 배포 전후 status count와 oldest age check가 정의되어 있습니다.

## Observability acceptance

아래 metric 또는 동등한 관측이 존재해야 합니다.

```text
alarm_dispatch_publish_batch_duration_seconds
alarm_dispatch_publish_duplicate_deliveries_total
alarm_dispatch_publish_hash_conflict_total
alarm_dispatch_wakeup_sent_total
alarm_dispatch_wakeup_failed_total
alarm_dispatch_pg_claimed_total
alarm_dispatch_pg_retry_scheduled_total
alarm_dispatch_pg_dlq_total
alarm_dispatch_pg_quarantined_total
alarm_dispatch_runner_empty_polls_total
alarm_dispatch_runner_wakeup_consumed_total
alarm_dispatch_runner_wakeup_timeout_total
alarm_dispatch_pg_oldest_pending_age_seconds
alarm_dispatch_pg_oldest_retry_age_seconds
alarm_dispatch_pg_oldest_sending_age_seconds
alarm_dispatch_pg_retention_deleted_rows_total
```

## Test acceptance

최소 테스트:

```bash
go test ./hololive/hololive-alarm-worker/internal/app -count=1
go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
go test ./hololive/hololive-dispatcher-go/internal/app -count=1
```

Integration gate:

```bash
TEST_DATABASE_URL=postgres://... go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
```

Scenario gate:

- Valkey wakeup disabled.
- Valkey wakeup timeout.
- Iris send timeout.
- MarkSent failure.
- worker restart with leased rows.
- stale sending quarantine.
- retention chunk cleanup.
