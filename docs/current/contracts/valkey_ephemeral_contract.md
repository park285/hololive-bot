# Valkey Ephemeral Contract

Valkey is an ephemeral cache, wakeup, index, and fast-path layer. PostgreSQL is the durable source of truth for alarm dispatch state.

## Allowed Valkey State

- Pure caches that can be rebuilt or skipped after restart.
- Derived alarm subscriber/member indexes that can be rebuilt from PostgreSQL.
- Payload-free wakeup signals for PostgreSQL-backed dispatch polling.
- Rate limit buckets where expiration or eviction is acceptable.

## Disallowed Valkey-Only State

- Alarm dispatch pending state.
- Retry, DLQ, quarantine, and terminal dedupe state.
- External send in-flight state.

## Dispatch Invariants

- `alarm_dispatch_events` stores room-agnostic payload once; `alarm_dispatch_deliveries` stores per-room state.
- `shadowed` delivery rows no longer exist: the v3 handoff was removed (`DEC-20260926-hololive-outbox-v3-convergence`) and the epoch-3 baseline status CHECK (`alarm_dispatch_deliveries_status_check`) no longer allows the value.
- `pending` and `retry` rows are the only claimable states.
- `leased`, `sending`, and terminal state updates must be owned by the worker that holds the row lease.
- External send failures after `sending` are ambiguous by default and must quarantine in the PG consumer path.
- `sent`, `dlq`, `quarantined`, and `cancelled` rows must not be reset to `pending` by dedupe conflict handling.
- Valkey wakeup uses a fixed list token, not Pub/Sub payload delivery. Wakeup loss must not lose a dispatch; PG fallback polling must still claim due rows.
- The same rule covers the delivery outbox wakeups `notification:delivery:wakeup` and `youtube:delivery:wakeup` (`QUEUE_AND_PUBSUB_CONTRACTS.md`): producers signal only after the PostgreSQL commit and never fail the write on a signal error.
