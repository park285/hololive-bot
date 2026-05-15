# Phase 01. PG Failure Semantics Hardening

## 목적

외부 전송 전 실패와 외부 전송 이후 실패를 명확히 분리합니다. PG-first에서 가장 중요한 정합성 개선입니다.

## 문제

현재 alarm-worker 내장 runner는 다음 순서로 동작합니다.

```text
render
→ MarkSending
→ SendMessage
→ MarkDispatched
```

렌더링 실패와 SendMessage 실패를 같은 실패 흐름으로 처리하면 안 됩니다.

## 결정

### Pre-send failure

`MarkSending` 이전 실패는 external send가 시작되지 않았으므로 retry/DLQ가 맞습니다.

예:

- render failure.
- invalid payload.
- message formatting failure.

상태:

```text
leased → retry
leased → dlq
```

### Post-sending failure

`MarkSending` 이후 `SendMessage` 실패는 external send outcome이 ambiguous합니다.

상태:

```text
sending → quarantined
```

자동 retry 금지입니다.

### MarkSent failure

`SendMessage` 성공 후 `MarkDispatched` 실패는 retry 금지입니다. 이미 메시지가 전송되었을 수 있습니다. row는 `sending`에 남기고 stale sending recovery가 quarantine하게 둡니다.

## Touch paths

```text
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner.go
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner_test.go
```

## No-touch paths

```text
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_terminal.go
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository.go
```

Repository status guard는 이미 의미가 맞으므로 바꾸지 않습니다.

## 설계

### Interface

`alarmDispatchConsumer`에 `Quarantine`을 직접 추가하면 Valkey legacy consumer까지 강제로 바꿔야 할 수 있습니다. 계약 유지와 legacy compatibility를 위해 optional sub-interface를 사용합니다.

```go
type alarmDispatchQuarantineConsumer interface {
    Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error
}
```

### Runner 분리

기존 `persistDispatchFailure`를 다음처럼 분리합니다.

```text
persistPreSendFailure
persistPostSendingFailure
```

### PG consumer 처리

```go
if consumer, ok := r.consumer.(alarmDispatchQuarantineConsumer); ok {
    return consumer.Quarantine(ctx, envelopes, reason)
}
```

Valkey consumer는 `Quarantine`을 구현하지 않으므로 기존 retry 흐름을 유지합니다.

## 테스트

### Unit

- render failure → `ScheduleRetry` 호출.
- `MarkSending` 이후 `SendMessage` failure + PG consumer mock → `Quarantine` 호출.
- `MarkSending` 이후 `SendMessage` failure + Valkey consumer mock → 기존 retry 호출.
- `MarkDispatched` failure → retry/DLQ/quarantine 호출 없음.
- `Quarantine` 실패 → runner error 반환.

### Integration

- PG delivery `leased` 상태에서 render failure → `retry`.
- PG delivery `sending` 상태에서 send failure → `quarantined`.
- stale sending recovery → `quarantined`.

## 완료 기준

- PG path에서 post-send failure가 자동 retry되지 않습니다.
- Valkey legacy path의 retry behavior는 유지됩니다.
- tests가 실패 시점별로 분리되어 있습니다.
