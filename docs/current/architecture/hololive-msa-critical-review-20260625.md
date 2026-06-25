# Hololive MSA critical review follow-up - 2026-06-25

## Scope

This note captures the code-level completion follow-up for the Hololive MSA hot path review. It is documentation and observability maintenance only. It does not change runtime behavior, migrations, CI policy, deploy manifests, or live database state.

## Alarm dispatch status literals

`alarm_dispatch_deliveries.status` is stored as lowercase text. Observability SQL must use these terminal literals:

```text
sent
dlq
cancelled
quarantined
```

Uppercase literals such as `SENT`, `DLQ`, `CANCELLED`, and `QUARANTINED` do not match the migration constraint and can produce empty terminal-status results.

## Terminal timestamp rule

Terminal alarm-dispatch views should derive terminal time from the status-specific timestamp columns before falling back to generic row timestamps:

```sql
coalesce(sent_at, dlq_at, cancelled_at, quarantined_at, updated_at, created_at)
```

This keeps `dlq`, `cancelled`, and `quarantined` rows visible in the same aggregate as `sent` rows without depending only on `updated_at`.

## Maintenance SQL

The matching read-only query set lives at:

```text
scripts/maintenance/hololive_msa_hot_path_observability.sql
```

It includes:

- terminal `alarm_dispatch_deliveries` counts using lowercase status literals
- active alarm dispatch backlog
- stuck community/shorts claim states
- duplicate community/shorts sent-state candidates
- sent tracking rows missing canonical alarm state
- `pg_stat_statements` hot query review for YouTube/alarm tables

## Operational boundary

This follow-up intentionally does not run the SQL against staging or production. Execute it manually in an approved read-only DB session when operational validation is requested.
