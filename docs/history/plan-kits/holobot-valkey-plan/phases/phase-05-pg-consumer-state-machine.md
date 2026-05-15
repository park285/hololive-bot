# Phase 05. PG consumer와 상태 머신 구현

## 목표

PostgreSQL `alarm_dispatch_deliveries`를 실제 dispatch source로 사용하는 consumer를 구현합니다. dispatcher는 Valkey wakeup 또는 fallback timeout 후 PG에서 due delivery를 claim하고, event payload를 distinct load한 뒤 Iris에 전송합니다.

이 phase의 핵심은 state transition을 엄격히 구현하는 것입니다.

## PG consumer high-level loop

```text
for service running:
    processed = drainDue(maxBatchesPerWake)
    if processed > 0:
        continue

    WaitValkeyWakeupOrTimeout()
    continue
```

`drainDue`:

```text
for i in 0..maxBatchesPerWake:
    deliveries = repo.ClaimDue(workerID, claimLimit, lease)
    if deliveries empty:
        return totalProcessed

    eventIDs = distinct(deliveries.event_id)
    events = repo.LoadEventsByID(eventIDs)
    groups = buildSendGroups(deliveries, events)

    for group in groups:
        processGroup(group)
```

## processGroup

```text
1. render 준비
2. render 실패 시:
     repo.ScheduleRetry 또는 repo.MoveToDLQ
     MarkSending 호출 금지
3. send 직전:
     repo.MarkSending(deliveryIDs, workerID, extendLease)
4. MarkSending이 모든 row를 업데이트하지 못하면:
     Iris send 금지
     log/metric 후 다음 batch
5. Iris send 호출
6. 성공:
     repo.MarkSent(deliveryIDs, workerID)
7. MarkSent 실패:
     즉시 retry 금지
     row는 sending으로 남기고 stale sending reconciliation이 quarantine
8. send error:
     명확한 pre-send/render error가 아니면 quarantine
```

## render 실패와 send 실패 구분

### render 실패

render 실패는 외부 send 전입니다. 따라서 retry 또는 DLQ로 보낼 수 있습니다.

예시:

```text
invalid payload schema -> dlq
missing event payload -> dlq 또는 retry, 원인에 따라 결정
temporary template dependency error -> retry
```

render 실패 시 `MarkSending`을 호출하면 안 됩니다.

### send 실패

send 실패는 “Iris 요청을 실제로 보냈는가?”에 따라 다릅니다.

```text
Iris request 생성 전 실패       -> retry 가능
connection acquire 전 실패       -> retry 가능
request write 시작 후 timeout    -> ambiguous, quarantine
connection reset                 -> ambiguous, quarantine
response 5xx received clearly    -> 정책상 retry 가능하지만 중복 가능성 검토 필요
response 4xx invalid payload     -> dlq
```

초기 production 기본값은 단순하고 안전하게 갑니다.

```text
send path에 진입한 뒤 error = quarantine
```

나중에 Iris idempotency가 생기면 retry 범위를 넓힙니다.

## group send 주의점

같은 room에 여러 delivery가 있으면 하나의 Iris message로 묶을 수 있습니다. 다만 group send는 idempotency 설계를 어렵게 만듭니다.

현재 정책:

- Iris idempotency 전: group send 후 ambiguous failure는 group 전체 quarantine
- Iris idempotency 후 per-delivery send로 전환하거나 send_attempt ledger 추가

초기 V3에서 send_attempt ledger는 만들지 않습니다. 따라서 group send retry는 보수적으로 다룹니다.

## MarkSending strictness

`MarkSending`은 모든 delivery id가 `leased + locked_by 일치`일 때만 send를 허용해야 합니다.

구현 예시:

```go
updated, err := repo.MarkSending(ctx, ids, workerID, extendLease)
if err != nil { return err }
if updated != len(ids) {
    metrics.MarkSendingConflict.Add(...)
    return nil // Iris send 금지
}
```

이렇게 하지 않으면 lease가 만료된 row를 다른 worker가 가져간 상태에서 중복 send할 수 있습니다.

## MarkSent strictness

`MarkSent`는 모든 delivery id가 `sending + locked_by 일치`일 때만 sent로 바꿉니다.

MarkSent 실패 후 즉시 retry하지 않습니다. 이미 Iris send는 성공했을 수 있습니다.

## stale 상태 reconciliation과 연결

`leased`는 lease 만료 후 retry됩니다.

`sending`은 stale 기준 시간이 지나면 quarantine됩니다.

이 phase에서 reconciliation job을 모두 구현하지 않더라도, 상태 머신은 reconciliation이 처리할 수 있게 row를 남겨야 합니다.

## workerID

`locked_by`는 process instance별로 충분히 unique해야 합니다.

권장 형식:

```text
{service_name}:{hostname}:{pid}:{boot_timestamp_or_random_suffix}
```

같은 deployment에서 workerID가 중복되면 MarkSending/MarkSent strictness가 약해질 수 있습니다.

## metrics

필수 metric:

```text
alarm_dispatch_claim_total
alarm_dispatch_claim_empty_total
alarm_dispatch_claim_latency_seconds
alarm_dispatch_delivery_status_transition_total{from,to,reason}
alarm_dispatch_mark_sending_conflict_total
alarm_dispatch_mark_sent_error_total
alarm_dispatch_send_success_total
alarm_dispatch_send_error_total{class}
alarm_dispatch_quarantine_total{reason}
alarm_dispatch_dlq_total{reason}
alarm_dispatch_retry_scheduled_total{reason}
```

label에 delivery_id/event_key/room_id를 넣지 않습니다.

## 테스트

필수 test:

1. pending/retry만 claim됨
2. shadowed/sent/dlq/quarantined/cancelled는 claim되지 않음
3. delivery claim 후 distinct event id로 LoadEventsByID 호출
4. event payload missing이면 DLQ 또는 retry 정책 적용
5. render 실패 시 MarkSending 호출 안 됨
6. MarkSending conflict 시 Iris send 호출 안 됨
7. send 성공 후 MarkSent 호출
8. MarkSent 실패 시 retry 호출 안 됨
9. ambiguous send error는 quarantine
10. stale leased는 reconciliation에서 retry
11. stale sending은 reconciliation에서 quarantine
12. wakeup 없이 fallback scan으로 처리됨

## 완료 기준

- PG consumer가 실제 delivery를 claim하고 전송 가능
- event payload join 중복 없음
- state transition이 repository 조건과 일치
- ambiguous send retry 금지
- feature flag 기본값은 아직 안전하게 유지

## no-go 조건

- shadowed row를 claim함
- ClaimDue에서 event payload를 join함
- MarkSending 없이 Iris send를 호출함
- MarkSent가 leased row도 sent로 바꿈
- send timeout을 자동 retry함
- MarkSent 실패 후 즉시 retry함

## LLM 작업 프롬프트

```text
PG consumer를 구현하세요.
consumer는 Valkey wakeup을 받거나 fallback timeout이 되면 PostgreSQL ClaimDue를 호출합니다.
ClaimDue 결과는 delivery row만 포함해야 하며, event payload는 distinct event_id로 LoadEventsByID를 호출해 가져오세요.
render 실패는 MarkSending 전이므로 retry/DLQ로 처리합니다.
Iris send 직전에 MarkSending을 호출하고, 모든 delivery id가 업데이트된 경우에만 send하세요.
send 성공 후 MarkSent를 호출합니다.
MarkSent 실패 후 즉시 retry하지 마세요. stale sending reconciliation이 quarantine하게 둡니다.
Iris idempotency가 없으므로 send path 진입 후 ambiguous error는 기본 quarantine입니다.
shadowed row는 절대 claim하지 마세요.
```
