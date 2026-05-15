# Appendix. Non-goals and No-touch Paths

## Non-goals

이번 작업은 다음을 하지 않습니다.

- Alarm HTTP API migration.
- Queue envelope version migration.
- PG schema migration.
- Valkey queue key rename.
- YouTube outbox contract rewrite.
- notification delivery outbox rewrite.
- Admin UI rewrite.
- legacy Valkey queue replay into PG.
- shadowed row promotion.
- duplicate-risk ack 없는 quarantine replay.

## No-touch paths

```text
docs/current/contracts/alarm.md
docs/current/contracts/valkey_ephemeral_contract.md
hololive/hololive-shared/pkg/contracts/alarm/*
hololive/hololive-shared/pkg/domain/alarm.go
hololive/hololive-shared/pkg/domain/alarm_dispatch_source.go
hololive/hololive-kakao-bot-go/scripts/migrations/058_create_alarm_dispatch_outbox.sql
hololive/hololive-kakao-bot-go/scripts/migrations/059_harden_alarm_dispatch_outbox.sql
```

## Read-only reference paths

```text
hololive/hololive-dispatcher-go/internal/app/runtime.go
hololive/hololive-dispatcher-go/internal/app/runtime_wakeup.go
hololive/hololive-dispatcher-go/internal/app/config.go
hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go
scripts/runtime/alarm-dispatch-outbox-retention.sh
scripts/runtime/alarm-dispatch-outbox-requeue.sh
```

## Separate RFC required

다음 작업은 별도 RFC가 필요합니다.

- 새로운 envelope version 도입.
- 새로운 PG status 도입.
- outbox table partitioning.
- logical replication 또는 LISTEN/NOTIFY 도입.
- Valkey Stream 기반 queue로 재전환.
- Iris idempotency key contract 추가.
