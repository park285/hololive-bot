# Phase 02. PG Idle Wakeup Waiter

## 목적

alarm-worker 내장 PG consumer의 idle DB polling을 제거하고, Valkey wakeup을 실제로 소비하게 만듭니다.

## 문제

Valkey consumer는 `DrainBatch` 내부에서 block pop을 합니다. 하지만 PG consumer는 `ClaimDue` query를 수행합니다. 따라서 empty batch 후 25ms sleep은 PG idle polling storm이 될 수 있습니다.

## 결정

PG mode에서만 idle waiter를 주입합니다.

```text
empty batch
  → wait alarm:dispatch:wakeup
  → wakeup received: reset backoff and claim immediately
  → timeout: increase backoff
  → Valkey unavailable: bounded polling fallback
```

## Touch paths

```text
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner.go
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_idle.go
hololive/hololive-alarm-worker/internal/app/build_egress.go
hololive/hololive-alarm-worker/internal/app/env.go
hololive/hololive-alarm-worker/internal/app/alarm_dispatch_runner_test.go
```

## Reuse existing contract

기존 wakeup key를 사용합니다.

```text
alarm:dispatch:wakeup
```

새 queue key를 만들지 않습니다.

## Idle waiter defaults

```text
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
ALARM_DISPATCH_IDLE_BACKOFF_MIN_MS=250
ALARM_DISPATCH_IDLE_BACKOFF_MAX_MS=5000
```

해석:

- wakeup enabled + Valkey available: max wait는 backoff 값이지만 token 수신 즉시 깨어납니다.
- wakeup disabled/unavailable: `POLL_INTERVAL_MS`를 상한으로 사용합니다.
- 처리 성공 시 backoff reset.

## Max batches per wake

하나의 wakeup으로 너무 많은 batch를 연속 처리하면 다른 작업을 굶길 수 있습니다.

```text
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=20
```

동작:

```text
processed batch count >= maxBatchesPerWake
  → short yield
  → next loop
```

## 테스트

- empty batch에서 idle waiter `Wait` 호출.
- processed batch에서 idle waiter `Reset` 호출.
- wakeup success → 즉시 true.
- wakeup timeout → true, backoff 증가.
- ctx cancel → false.
- wakeup disabled → sleep fallback.
- `maxBatchesPerWake` 도달 → short yield.

## 완료 기준

- PG mode에서 25ms 고정 idle polling이 없습니다.
- Valkey wakeup token을 alarm-worker 내장 runner가 소비합니다.
- wakeup 장애 시 PG fallback polling으로 due row를 처리합니다.
- Valkey path의 기존 block pop behavior는 변경하지 않습니다.
