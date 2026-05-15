# Valkey Command Policy

## 기본 원칙

Valkey는 dispatch correctness source가 아닙니다. hot path에서는 고정 비용 또는 bounded 비용 명령만 사용합니다.

## 허용

```text
SET key value NX PX/EX ttl
GET bounded key
LPUSH alarm:dispatch:wakeup 1
BRPOP alarm:dispatch:wakeup timeout  # key exactly one
EXPIRE/PEXPIRE exact key
DEL exact dedup keys
UNLINK exact admin cleanup keys
```

## 금지

```text
KEYS
PUBLISH as default dispatch wakeup
SUBSCRIBE/PSUBSCRIBE as dispatch queue
SCAN without strict bounded cursor policy
LRANGE 0 -1
SMEMBERS on unbounded set
HGETALL on unbounded hash
Lua loop over unbounded data
```

## 예외 규칙

고복잡도 명령이 정말 필요하면 다음을 코드 주석과 문서에 남깁니다.

```text
- 왜 필요한가
- 어떤 key에만 적용되는가
- 데이터 크기 상한은 무엇인가
- 장애 시 대체 경로는 무엇인가
- metric/alert는 무엇인가
```
