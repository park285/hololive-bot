# T12. Reconciliation throttle

## 목적

stale recovery가 dispatcher hot path에 매번 끼지 않게 합니다.

## 작업 대상

- `dispatchoutbox/consumer.go`
- related tests

## 작업

1. `Consumer`에 `recoveryInterval`과 `lastRecoveryAttempt`를 추가합니다.
2. `DrainBatch()` 시작 시 `maybeRecover()`를 호출합니다.
3. interval 이내에는 recovery query를 실행하지 않습니다.
4. recovery error가 있어도 claim을 계속할지, iteration error로 볼지 정책을 정합니다.

## 권장 정책

- PG query error이면 iteration error.
- interval throttle로 빈번한 stale scan 방지.
- default interval: 30초.

## 완료 기준

- 100번 DrainBatch 호출 중 recovery는 interval 조건에 맞게 제한됩니다.
- recovery query는 limit을 유지합니다.
- stale leased/sending 정책은 유지됩니다.

## LLM 프롬프트

`DrainBatch()`마다 recovery query가 실행되지 않도록 throttle을 구현하십시오. bounded batch는 유지하십시오.
