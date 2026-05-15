# T01. Post-send Failure Quarantine

## 목표

PG consumer path에서 `MarkSending` 이후 `SendMessage` 실패를 자동 retry하지 않고 quarantine합니다.

## PATH

```text
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner.go
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner_test.go
```

## 변경 내용

- `persistDispatchFailure`를 `persistPreSendFailure`와 `persistPostSendingFailure`로 분리합니다.
- optional interface를 추가합니다.

```go
type alarmDispatchQuarantineConsumer interface {
    Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error
}
```

- PG consumer가 interface를 구현하면 post-send failure는 `Quarantine`으로 처리합니다.
- Valkey consumer는 기존 retry behavior를 유지합니다.

## 주의

- `alarmDispatchConsumer` 기본 interface에 `Quarantine`을 바로 추가하지 않습니다. 그러면 Valkey queue consumer까지 수정 범위가 늘어납니다.
- `MarkDispatched` 실패는 retry로 전환하지 않습니다.

## 테스트

- PG mock consumer with Quarantine.
- Valkey-like mock without Quarantine.
- Send failure after MarkSending.
- Render failure before MarkSending.
