# Prompt: PG mode Valkey degraded startup/readiness

## 목표

PG consumer mode에서 Valkey wakeup 장애가 dispatcher startup/readiness를 막지 않게 합니다.

## 대상 파일

- `hololive/hololive-dispatcher-go/internal/app/runtime.go`
- `hololive/hololive-dispatcher-go/internal/app/config.go`
- runtime/config tests

## 요구사항

1. `consumer_mode=pg`에서는 Valkey wakeup client 생성 실패를 fatal로 처리하지 않습니다.
2. cache client가 nil이면 `waitForPGDispatchSignal()`이 poll interval sleep fallback을 수행합니다.
3. readiness는 PG mode에서 Postgres와 Iris를 hard dependency로 유지합니다.
4. Valkey wakeup 상태는 `wakeup_degraded` 같은 field로 표시합니다.
5. `consumer_mode=valkey`에서는 기존처럼 Valkey가 필수입니다.

## 테스트

- pg mode + Valkey unavailable -> startup success.
- pg mode + Valkey unavailable -> ready or degraded-ready policy 확인.
- valkey mode + Valkey unavailable -> startup/readiness fail.
