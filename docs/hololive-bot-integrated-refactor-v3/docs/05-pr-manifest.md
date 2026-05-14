# 05. PR Manifest

## PR-01 shared logging foundation

수정:
- `shared-go/pkg/logging/attrs.go`
- `shared-go/pkg/logging/context.go`
- `shared-go/pkg/logging/id.go`
- `shared-go/pkg/logging/log.go`
- `shared-go/pkg/logging/operation.go`
- `shared-go/pkg/logging/sanitize.go`

검증:
```bash
gofmt -w shared-go/pkg/logging
go test ./shared-go/pkg/logging
```

## PR-02 HTTP request context

수정:
- `hololive/hololive-shared/pkg/server/middleware/security.go`
- `hololive/hololive-shared/pkg/server/middleware/logger.go`

검증:
```bash
gofmt -w hololive/hololive-shared/pkg/server/middleware
go test ./hololive/hololive-shared/pkg/server/middleware
```

## PR-03 bot command flow

수정:
- `hololive/hololive-kakao-bot-go/internal/bot/log_events.go`
- `hololive/hololive-kakao-bot-go/internal/bot/log_attrs.go`
- `hololive/hololive-kakao-bot-go/internal/bot/bot_ingress.go`
- `hololive/hololive-kakao-bot-go/internal/bot/command_router.go`
- `hololive/hololive-kakao-bot-go/internal/bot/bot_message_handler.go`
- `hololive/hololive-kakao-bot-go/internal/bot/bot_message_async.go`

검증:
```bash
gofmt -w hololive/hololive-kakao-bot-go/internal/bot
go test ./hololive/hololive-kakao-bot-go/internal/bot
```

## PR-04 bot lifecycle

수정:
- `hololive/hololive-kakao-bot-go/internal/bot/bot_lifecycle.go`

## PR-05 alarm scheduler

수정:
- `hololive/hololive-alarm-worker/internal/service/alarm/scheduler/runtime_scheduler_events.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/scheduler/runtime_scheduler_loop.go`

## PR-06 alarm egress/outbox

수정 대상은 현재 outbox/egress 구현 파일을 먼저 inventory한 뒤 진행합니다.

## PR-07 dispatcher flow

수정:
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatch_events.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatch_attrs.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher_retry.go`

## PR-08 llm scheduler

수정:
- `hololive/hololive-llm-sched/internal/app/runtime_observability.go`
- provider/prompt/result boundary 파일

## PR-09 ingestion/youtube scraper

수정:
- `hololive/hololive-stream-ingester/internal/runtime/ingestion_events.go`
- bootstrap/runtime scheduler/poller/outbox 파일

## PR-10 remote `/logs` mirror

수정:
- `scripts/logs/remote-sync-main-logs.sh`
- `scripts/systemd/hololive-main-log-mirror@.service`
- `scripts/systemd/hololive-main-log-mirror@.timer`

## PR-11 guardrails

수정:
- `scripts/refactor/validate-no-admin-touch.sh`
- `scripts/refactor/grep-sensitive-logs.sh`
- `scripts/refactor/test-non-admin-go.sh`
