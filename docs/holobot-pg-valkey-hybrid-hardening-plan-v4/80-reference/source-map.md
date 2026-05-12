# Source Map

현재 구현 검토에 사용한 주요 파일입니다.

```text
hololive/hololive-shared/pkg/service/alarm/queue/publisher.go
  Publish, PublishBatch, pg_first, wakeup token

hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository.go
  InsertBatch, ClaimDue, LoadEventsByID, MarkSending, MarkSent, retry/DLQ/quarantine

hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/consumer.go
  PG consumer DrainBatch, rehydrate delivery context, reconciliation calls

hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/dedupe_key.go
  event_key/dedupe_key construction

hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/model.go
  status, repository interfaces, result structs

hololive/hololive-dispatcher-go/internal/app/runtime.go
  dispatcher runtime, PG mode, wakeup wait, readiness

hololive/hololive-dispatcher-go/internal/app/config.go
  mode validation, PG/Valkey config

hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go
  RunOnceProcessed, MarkSending, Iris send, MarkDispatched, failure policy

hololive/hololive-dispatcher-go/internal/dispatch/grouping.go
  room group construction

hololive/hololive-alarm-worker/internal/service/alarm/checker/notifier.go
  dedup claim, batch publish, mark published

hololive/hololive-kakao-bot-go/scripts/migrations/058_create_alarm_dispatch_outbox.sql
  events/deliveries schema and indexes
```
