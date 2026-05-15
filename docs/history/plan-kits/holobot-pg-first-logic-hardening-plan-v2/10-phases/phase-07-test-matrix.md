# Phase 07. Test Matrix and Verification

## Unit tests

### Failure semantics

| Case | Expected |
|---|---|
| render failure before `MarkSending` | `ScheduleRetry` or `MoveToDLQ` |
| `MarkSending` failure | runner error, no external send |
| `SendMessage` failure after `MarkSending`, PG consumer | `Quarantine` |
| `SendMessage` failure after `MarkSending`, Valkey consumer | legacy retry |
| `MarkDispatched` failure after successful send | no retry, return error |
| exhausted retry before send | DLQ and claim release |

### Idle waiter

| Case | Expected |
|---|---|
| empty batch PG mode | idle waiter `Wait` |
| processed batch | idle waiter `Reset` |
| wakeup token consumed | immediate next loop |
| wakeup timeout | backoff increase |
| wakeup error | fallback sleep |
| context canceled | stop loop |
| wakeup disabled | poll interval fallback |

### Config parity

| Case | Expected |
|---|---|
| env unset | safe defaults |
| invalid duration env | fallback |
| pg mode | dispatchoutbox consumer with lease/recovery options |
| valkey mode | queue consumer unchanged |
| max batches per wake | yield after threshold |

### Retention

| Case | Expected |
|---|---|
| advisory lock unavailable | skip |
| sent older than retention | delete up to limit |
| pending older than retention | not deleted |
| orphan event | delete |
| event with delivery | not deleted |
| limit > max | clamp |
| query timeout | error metric/log |

## Integration tests

Use `TEST_DATABASE_URL`.

1. Insert pending deliveries.
2. Claim batch.
3. Assert status `leased`.
4. Render failure path → retry.
5. Mark sending then send failure → quarantine.
6. Mark sending then MarkSent success → sent.
7. Expired leased → retry recovery.
8. Stale sending → quarantine recovery.
9. Duplicate dedupe key → no new delivery.
10. Retention cleanup of terminal rows.
11. Orphan event cleanup.
12. Valkey wakeup disabled → poll fallback.

## Load tests

### Burst publish

```text
1,000 deliveries in 60 seconds
```

Expected:

- No hash conflict.
- pending oldest age under threshold.
- inserted/processed ratio stable.

### Duplicate storm

```text
10,000 duplicate publish attempts
```

Expected:

- duplicate counter increases.
- no unbounded DB lock wait.
- no duplicate sent rows.

### Wakeup down

Disable Valkey wakeup.

Expected:

- PG fallback polling processes due rows.
- latency increases only up to poll interval/backoff bound.
- no dispatch loss.

### Iris timeout

Inject 10% timeout.

Expected:

- post-send ambiguous failures go quarantine in PG path.
- no automatic duplicate retry.
- alert fires.

### Worker restart

Kill alarm-worker while rows are `leased` and `sending`.

Expected:

- expired leased → retry.
- stale sending → quarantine.
- no duplicate sends from sending rows.

## Verification commands

```bash
go test ./hololive/hololive-alarm-worker/internal/app -count=1
go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
go test ./hololive/hololive-dispatcher-go/internal/app -count=1
```

```bash
TEST_DATABASE_URL=postgres://... go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
```

## 완료 기준

- failure timing별 test가 있습니다.
- wakeup/fallback test가 있습니다.
- retention test가 있습니다.
- worker restart scenario가 검증됩니다.
