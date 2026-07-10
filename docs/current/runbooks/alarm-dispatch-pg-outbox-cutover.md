# Alarm Dispatch PG Ledger Cutover

> Status: cutover COMPLETE. The PG dispatch outbox is the sole, unconditional publish and consume path.
> The legacy `valkey_only`/`shadow` publish arms and the `valkey` consumer arm have been REMOVED from the
> code, along with `ALARM_DISPATCH_PUBLISH_MODE`, `ALARM_DISPATCH_CONSUMER_MODE`, and
> `ALARM_DISPATCH_SHADOW_FATAL`. Env-flip rollback is gone; rollback is now "redeploy the previous image".
> The sections below are retained as operational history and steady-state PG guidance.

## Modes

There is no mode selection anymore. PG publish (pending-row insert + Valkey wakeup) and the PG consumer are
unconditional. The three former mode variables are no longer read; setting `ALARM_DISPATCH_PUBLISH_MODE` or
`ALARM_DISPATCH_CONSUMER_MODE` to a removed legacy value (`valkey_only`, `shadow`, `valkey`) is a startup
error. `pg_first`/`pg`/unset are accepted as no-ops for one redeploy of grace.

Publisher (steady-state):

```text
ALARM_DISPATCH_WAKEUP_ENABLED=true|false
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=1000
```

Dispatcher (steady-state):

```text
ALARM_DISPATCH_MAX_BATCH=50
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_QUARANTINE_THRESHOLD_SECONDS=180  # 미설정 시 3×lease, lease 미만은 lease로 clamp
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

Former mode matrix (REMOVED — historical only):

| publisher | consumer | state | status |
|---|---|---|---|
| `valkey_only` | `valkey` | legacy production path | removed |
| `shadow` | `valkey` | PG observation only | removed |
| `pg_first` | `pg` | PG ledger + Valkey wakeup hybrid | now the only, unconditional path |

## Steady-State Notes (post-cutover)

1. Migrations through `059_harden_alarm_dispatch_outbox.sql` are applied.
2. PostgreSQL integration gate:

```bash
TEST_DATABASE_URL=postgres://... go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
```

3. The publisher writes `pending` rows directly and emits a payload-free Valkey wakeup; the PG consumer
   claims them under lease. Watch `pending`, `leased`, `sending`, `sent`, `retry`, `dlq`, and `quarantined`
   counts.

Setting a removed legacy mode value is a startup error. Treat a failed alarm-worker start on a stale
legacy mode value as safer than silently running the wrong producer; clear the variable and redeploy.

## Logic Hardening Gate

Before deploying alarm-worker logic hardening, verify these checks:

```bash
go test ./hololive/hololive-alarm-worker/internal/app -count=1
go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
```

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml config \
  | grep -E 'ALARM_DISPATCH_(WAKEUP|MAX_BATCH|POLL|LEASE|RECOVERY|RETENTION|IDLE)'
```

`ALARM_DISPATCH_PUBLISH_MODE`/`ALARM_DISPATCH_CONSUMER_MODE`/`ALARM_DISPATCH_SHADOW_FATAL` are intentionally
absent from compose — PG is the only path and these variables are no longer read.

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

`dedupe_key NOT LIKE 'v2:%'` identifies pre-v2 PG delivery rows written before the stable room/event dedupe key. The current publisher inserts only v2 rows and relies on `ON CONFLICT (dedupe_key) DO NOTHING`; it does not compare pre-v2 delivery keys. Before deploying an image that removes the old comparison path, this read-only query must show zero active pre-v2 rows (`pending`, `leased`, `retry`, `sending`). Terminal residue (`sent`, `dlq`, `quarantined`, `cancelled`) is not an active send path, but final Legacy Fadeout acceptance still requires retention/cleanup to reduce the total pre-v2 count to zero and a repeat read-only check showing no rows. Do not backfill or rewrite these keys without a duplicate-risk review, and do not promote legacy `shadowed` rows to `pending`.

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

## Rollback

The env-flip rollback to a Valkey publish/consume path has been REMOVED. There is no `valkey_only`/`shadow`/
`valkey` mode to switch back to. Rollback now means redeploying the previous alarm-worker image. The legacy
rollback window is closed by explicit decision.

| rollback path | allowed | required accounting |
|---|---|---|
| stop producer, keep PG consumer running, drain PG | yes | record active status counts and oldest ages before and after drain |
| redeploy previous image (image-level rollback) | yes, with operator approval | the prior image still expects PG; do not reintroduce legacy mode env |

Never promote any historical `shadowed` rows to `pending`. Never automatically retry `sending` rows. Never
replay `quarantined` rows without explicit duplicate-risk acknowledgement.
