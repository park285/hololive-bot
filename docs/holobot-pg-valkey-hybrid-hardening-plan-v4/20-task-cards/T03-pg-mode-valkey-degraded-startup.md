# T03. PG mode에서 Valkey degraded startup/readiness 허용

## 목적

Valkey wakeup 장애가 PG fallback scan을 막지 않게 합니다.

## 현재 문제

PG ledger가 source of truth인데 dispatcher가 PG mode에서도 Valkey 연결 실패로 startup/readiness fail하면, Valkey 장애 시 PG fallback scan을 수행할 수 없습니다.

## 작업 대상

- `hololive/hololive-dispatcher-go/internal/app/runtime.go`
- `hololive/hololive-dispatcher-go/internal/app/config.go`
- dispatcher readiness tests

## 작업

1. `consumer_mode=pg`에서는 Valkey wakeup client 생성 실패를 fatal로 보지 않는 옵션을 추가합니다.
2. wakeup client가 없으면 `waitForPGDispatchSignal()`은 sleep fallback만 수행합니다.
3. readiness는 PG mode에서 Postgres + Iris + loop를 hard requirement로 보고, Valkey는 degraded field로 표시합니다.
4. `consumer_mode=valkey`에서는 Valkey가 여전히 hard requirement입니다.

## 완료 기준

- PG mode + Valkey unavailable에서 runtime이 기동 가능합니다.
- `/ready`는 `wakeup_degraded=true` 같은 정보를 반환하지만 not_ready로 떨어지지 않습니다.
- Valkey mode + Valkey unavailable은 여전히 startup/readiness fail입니다.

## LLM 프롬프트

PG consumer mode에서 Valkey wakeup은 latency helper로만 동작하도록 runtime/readiness를 수정하십시오. correctness dependency인 Postgres와 Iris는 hard requirement로 유지하십시오.
