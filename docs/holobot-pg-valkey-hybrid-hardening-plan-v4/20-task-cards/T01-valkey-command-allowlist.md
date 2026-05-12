# T01. Valkey command allowlist 테스트

## 목적

알람 dispatch hot path에서 고복잡도 Valkey 명령이 재도입되지 않게 합니다.

## 허용 hot path 명령

```text
SET NX PX/EX
GET small key
LPUSH one element
BRPOP one fixed key
EXPIRE/PEXPIRE
DEL exact keys for dedup release only
```

## 금지 hot path 명령

```text
KEYS
PUBLISH
SUBSCRIBE/PSUBSCRIBE as dispatch wakeup
SCAN without strict bound
LRANGE 0 -1
SMEMBERS on unbounded set
HGETALL on unbounded hash
Lua loop over variable-size data
```

## 작업 대상

- `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go`
- `hololive/hololive-dispatcher-go/internal/app/runtime.go`
- 관련 fake cache/mocks test

## 완료 기준

- pg_first publish wakeup에서 `PUBLISH`가 호출되지 않습니다.
- wakeup `LPUSH`는 element 1개만 사용합니다.
- dispatcher `BRPOP`은 key 1개만 사용합니다.
- `KEYS` 사용 테스트가 있으면 실패합니다.

## LLM 프롬프트

알람 dispatch hot path에서 실행되는 Valkey command를 fake client로 기록하는 테스트를 작성하십시오. allowlist 외 명령이 호출되면 테스트가 실패하게 하십시오.
