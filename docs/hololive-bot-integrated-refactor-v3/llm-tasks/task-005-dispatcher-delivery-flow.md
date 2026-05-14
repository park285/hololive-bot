# Task 005. dispatcher-go delivery flow

## 목표

legacy dispatcher라도 delivery boundary를 명확히 기록한다.

## 수정 파일

- `hololive/hololive-dispatcher-go/internal/dispatch/dispatch_events.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatch_attrs.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher_retry.go`

## 분리 이벤트

```text
dispatch.batch.drain.*
dispatch.group.render.*
dispatch.group.send.*
dispatch.group.mark_sending.failed
dispatch.group.mark_dispatched.failed
dispatch.group.retry.scheduled
dispatch.group.dlq.moved
dispatch.group.quarantined
```
