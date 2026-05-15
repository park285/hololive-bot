# Alarm Dispatch PG Ledger Cutover

## Modes

Publisher:

```text
ALARM_DISPATCH_PUBLISH_MODE=valkey_only|shadow|pg_first
ALARM_DISPATCH_SHADOW_FATAL=false|true
ALARM_DISPATCH_WAKEUP_ENABLED=true|false
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=1000
```

Dispatcher:

```text
ALARM_DISPATCH_CONSUMER_MODE=valkey|pg
ALARM_DISPATCH_MAX_BATCH=50
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=20
ALARM_DISPATCH_IDLE_BACKOFF_MIN_MS=250
ALARM_DISPATCH_IDLE_BACKOFF_MAX_MS=5000
ALARM_DISPATCH_RECOVERY_INTERVAL_MS=30000
ALARM_DISPATCH_RECOVERY_BATCH_SIZE=100
ALARM_DISPATCH_WAKEUP_ENABLED=true|false
ALARM_DISPATCH_RETENTION_ENABLED=true
ALARM_DISPATCH_RETENTION_INTERVAL_MS=3600000
ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS=30000
ALARM_DISPATCH_RETENTION_LIMIT=1000
```

Allowed steady-state pairs:

| publisher | consumer | state |
|---|---|---|
| `valkey_only` | `valkey` | legacy production path |
| `shadow` | `valkey` | PG observation only |
| `pg_first` | `pg` | PG ledger + Valkey wakeup hybrid |

Forbidden pairs:

| publisher | consumer | reason |
|---|---|---|
| `pg_first` | `valkey` | PG rows are written but legacy dispatcher never claims them |
| `shadow` | `pg` | shadow rows are observation-only and must not be promoted automatically |
| empty/unknown | `pg` | PG consumer requires explicit `pg_first` peer mode |

## Safe Sequence

1. Apply migrations through `059_harden_alarm_dispatch_outbox.sql`.
2. Run the PostgreSQL integration gate against a disposable or staging database:

```bash
TEST_DATABASE_URL=postgres://... go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
```

3. Start `shadow` publisher mode with dispatcher still in `valkey` mode.
4. Confirm only `shadowed` rows increase in `alarm_dispatch_deliveries`.
5. Drain or explicitly account for legacy Valkey queue/retry residue.
6. Switch publisher to `pg_first` and dispatcher to `pg` in the same rollout window.
7. Watch `pending`, `leased`, `sending`, `sent`, `retry`, `dlq`, and `quarantined` counts.

Do not perform full rollout until the P1-P4 hardening gates are complete: set-based insert, Valkey degraded PG readiness, reconciliation throttle, batch terminal updates, and retention indexes.

Do not run `publisher=pg_first, consumer=valkey` or `publisher=valkey_only/shadow, consumer=pg` as a steady state.
Unknown publisher modes are startup errors. Treat a failed alarm-worker start during cutover as safer than silently falling back to the wrong producer mode.

## Logic Hardening Gate

Before deploying alarm-worker logic hardening, verify these checks:

```bash
go test ./hololive/hololive-alarm-worker/internal/app -count=1
go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
go test ./hololive/hololive-dispatcher-go/internal/app -count=1
```

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml config \
  | grep -E 'ALARM_DISPATCH_(PUBLISH_MODE|CONSUMER_MODE|WAKEUP|MAX_BATCH|POLL|LEASE|RECOVERY|RETENTION|IDLE)'
```

Required hardening conditions:

- PG consumer post-send `SendMessage` failures quarantine instead of retrying.
- `MarkDispatched`/mark-sent failures after external send do not schedule retry.
- alarm-worker PG consumer waits on `alarm:dispatch:wakeup` when idle and falls back to bounded polling.
- `ALARM_DISPATCH_MAX_BATCHES_PER_WAKE` bounds continuous batch processing.
- retention is chunked, query-time-limited, and protected by a PostgreSQL advisory lock.

## Checks

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

If any legacy residue is non-zero, stop the cutover. Either let the Valkey dispatcher drain it before switching, or record the residue and accept that those legacy items are owned by the rollback/drain procedure. Do not replay legacy Valkey residue into PG and do not promote `shadowed` rows to `pending`.

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;

SELECT COALESCE(EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at)), 0) AS oldest_pending_age_seconds
FROM alarm_dispatch_deliveries
WHERE status = 'pending'
  AND next_attempt_at <= NOW();

SELECT COALESCE(EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at)), 0) AS oldest_retry_age_seconds
FROM alarm_dispatch_deliveries
WHERE status = 'retry'
  AND next_attempt_at <= NOW();

SELECT COALESCE(EXTRACT(EPOCH FROM NOW() - MIN(sending_started_at)), 0) AS oldest_sending_age_seconds
FROM alarm_dispatch_deliveries
WHERE status = 'sending';

SELECT status, count(*)
FROM alarm_dispatch_deliveries
WHERE dedupe_key NOT LIKE 'v2:%'
GROUP BY status
ORDER BY status;
```

`dedupe_key NOT LIKE 'v2:%'` identifies pre-v2 PG delivery rows written before the stable room/event dedupe key. The publisher checks the corresponding legacy key before inserting a v2 row, so these rows are treated as duplicates instead of creating a second delivery. Before cutover, account for any non-zero legacy PG rows by status and let retention expire terminal rows. Do not backfill or rewrite these keys without a duplicate-risk review, and do not promote legacy `shadowed` rows to `pending`.

Event payload is stored once in `alarm_dispatch_events`; room state lives in `alarm_dispatch_deliveries`. Do not inspect or replay from a single per-room JSON outbox table.

## Recovery

Stale `leased` rows can be retried because external send has not started. Stale `sending` rows default to quarantine because external send outcome is ambiguous. Quarantined `sending` rows require operator inspection before replay.

Manual requeue requires an explicit duplicate-risk acknowledgement and writes an audit row:

```bash
DATABASE_URL=... ./scripts/runtime/alarm-dispatch-outbox-requeue.sh \
  <delivery-id> <operator-id> "<reason>" force_duplicate_risk_ack=true
```

Retention cleanup must be chunked. Run one status at a time and repeat only while the returned row count remains at the chosen limit:

```bash
DATABASE_URL=... ./scripts/runtime/alarm-dispatch-outbox-retention.sh sent 90 1000
DATABASE_URL=... ./scripts/runtime/alarm-dispatch-outbox-retention.sh dlq 180 500
DATABASE_URL=... ./scripts/runtime/alarm-dispatch-outbox-retention.sh quarantined 180 500
```

The alarm-worker maintenance runner performs the same cleanup automatically when `ALARM_DISPATCH_RETENTION_ENABLED=true`. Keep the manual script as an emergency fallback.

## Rollback Matrix

| rollback path | allowed | required accounting |
|---|---|---|
| stop producer, keep `consumer=pg`, drain PG | yes | record active status counts and oldest ages before and after drain |
| `pg_first/pg` to `valkey_only/valkey` | only with operator approval | record stranded PG `pending/retry/leased/sending` rows and legacy Valkey residue |
| `pg_first/valkey` | no | creates stranded PG rows |
| `shadow/pg` | no | `shadowed` rows are observation-only |
| `valkey_only/pg` | no | PG consumer has no durable producer |

Never replay legacy Valkey residue into PG. Never promote `shadowed` rows to `pending`. Never automatically retry `sending` rows. Never replay `quarantined` rows without explicit duplicate-risk acknowledgement.
