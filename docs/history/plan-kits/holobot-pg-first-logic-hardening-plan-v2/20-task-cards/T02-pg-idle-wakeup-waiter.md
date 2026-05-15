# T02. PG Idle Wakeup Waiter

## 목표

alarm-worker 내장 PG consumer가 empty batch에서 25ms 고정 polling을 하지 않도록 합니다.

## PATH

```text
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner.go
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_idle.go
hololive/hololive-alarm-worker/internal/app/build_egress.go
hololive/hololive-alarm-worker/internal/app/env.go
```

## 변경 내용

- `alarmDispatchIdleWaiter` interface 추가.
- PG mode에서 idle waiter 주입.
- `alarm:dispatch:wakeup`을 `BRPOP`으로 대기.
- wakeup timeout/error 시 bounded fallback polling.
- processed batch 후 backoff reset.

## Defaults

```text
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
ALARM_DISPATCH_IDLE_BACKOFF_MIN_MS=250
ALARM_DISPATCH_IDLE_BACKOFF_MAX_MS=5000
```

## 테스트

- wait success.
- timeout.
- context cancel.
- wakeup disabled.
- invalid env fallback.
