# Valkey Ephemeral Contract

Valkey is an ephemeral cache, wakeup, index, and fast-path layer. PostgreSQL is the durable source of truth for alarm dispatch state.

## Allowed Valkey State

- Pure caches that can be rebuilt or skipped after restart.
- Derived alarm subscriber/member indexes that can be rebuilt from PostgreSQL.
- Payload-free wakeup signals for PostgreSQL-backed dispatch polling.
- Rate limit buckets and admin sessions where expiration or eviction is acceptable.

## Disallowed Valkey-Only State

- Alarm dispatch pending state.
- Retry, DLQ, quarantine, and terminal dedupe state.
- External send in-flight state.

## Dispatch Invariants

- `alarm_dispatch_events` stores room-agnostic payload once; `alarm_dispatch_deliveries` stores per-room state.
- `shadowed` delivery rows are observation-only and must not be claimed by the PG consumer.
- `pending` and `retry` rows are the only claimable states.
- `leased`, `sending`, and terminal state updates must be owned by the worker that holds the row lease.
- External send failures after `sending` are ambiguous by default and must quarantine in the PG consumer path.
- `sent`, `dlq`, `quarantined`, and `cancelled` rows must not be reset to `pending` by dedupe conflict handling.
- Valkey wakeup uses a fixed list token, not Pub/Sub payload delivery. Wakeup loss must not lose a dispatch; PG fallback polling must still claim due rows.
- Legacy Valkey queue consumption and PG ledger consumption must not process the same newly published event at the same time.
