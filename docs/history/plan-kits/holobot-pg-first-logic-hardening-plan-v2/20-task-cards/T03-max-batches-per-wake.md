# T03. Max Batches Per Wake

## 목표

한 번 깨어난 runner가 무제한으로 batch를 연속 처리하지 않게 제한합니다.

## PATH

```text
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner.go
hololive/hololive-alarm-worker/internal/app/build_egress.go
docker-compose.prod.yml
```

## 변경 내용

- `alarmDispatchLoopState` 추가.
- processed batch count 추적.
- `ALARM_DISPATCH_MAX_BATCHES_PER_WAKE` 도달 시 short yield.

## Default

```text
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=20
```

## 테스트

- threshold 미만은 즉시 다음 loop.
- threshold 도달 시 yield.
- context cancel 시 중단.
