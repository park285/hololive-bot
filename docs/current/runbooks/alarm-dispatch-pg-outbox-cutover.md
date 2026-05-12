# Alarm Dispatch PG Ledger Cutover

## Modes

Publisher:

```text
ALARM_DISPATCH_PUBLISH_MODE=valkey_only|shadow|pg_first
ALARM_DISPATCH_SHADOW_FATAL=false|true
ALARM_DISPATCH_WAKEUP_ENABLED=true|false
```

Dispatcher:

```text
ALARM_DISPATCH_CONSUMER_MODE=valkey|pg
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
ALARM_DISPATCH_WAKEUP_ENABLED=true|false
```

## Safe Sequence

1. Apply migration `058_create_alarm_dispatch_outbox.sql`, which creates `alarm_dispatch_events` and `alarm_dispatch_deliveries`.
2. Start `shadow` publisher mode with dispatcher still in `valkey` mode.
3. Confirm only `shadowed` rows increase in `alarm_dispatch_deliveries`.
4. Drain or explicitly account for legacy Valkey queue/retry residue.
5. Switch publisher to `pg_first` and dispatcher to `pg` in the same rollout window.
6. Watch `pending`, `leased`, `sending`, `sent`, `retry`, `dlq`, and `quarantined` counts.

Do not run `publisher=pg_first, consumer=valkey` or `publisher=valkey_only/shadow, consumer=pg` as a steady state.
Unknown publisher modes are startup errors. Treat a failed alarm-worker start during cutover as safer than silently falling back to the wrong producer mode.

## Checks

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
```

Event payload is stored once in `alarm_dispatch_events`; room state lives in `alarm_dispatch_deliveries`. Do not inspect or replay from a single per-room JSON outbox table.

## Recovery

Stale `leased` rows can be retried because external send has not started. Stale `sending` rows default to quarantine because external send outcome is ambiguous. Quarantined `sending` rows require operator inspection before replay.

Manual requeue requires an explicit duplicate-risk acknowledgement and writes an audit row:

```bash
DATABASE_URL=... ./scripts/runtime/alarm-dispatch-outbox-requeue.sh \
  <delivery-id> <operator-id> "<reason>" force_duplicate_risk_ack=true
```
