# Appendix. Valkey command complexity policy

## 1. 적용 범위

이 정책은 alarm dispatch hot path에 우선 적용합니다.

hot path:

```text
publisher pg_first wakeup
dispatcher wakeup wait
dispatcher fallback loop 주변 cache access
중복 발송 방지와 직접 연결된 path
```

non-hot path:

```text
admin cleanup
index rebuild
low-frequency diagnostics
migration helper
manual investigation
```

non-hot path에서도 unbounded 명령은 가능하면 피합니다. 꼭 필요하면 주석, limit, 운영 절차가 필요합니다.

## 2. 기본 허용 command

### exact-key read/write

```text
GET key
SET key value [NX|XX] [PX ttl|EX ttl]
EXISTS key
TTL/PTTL key
EXPIRE/PEXPIRE key ttl
UNLINK exact-key
```

주의:

- `UNLINK`는 exact key cleanup/admin 용도입니다.
- pattern delete는 금지입니다.

### wakeup list

```text
LPUSH alarm:dispatch:wakeup 1
BRPOP alarm:dispatch:wakeup timeout
```

주의:

- `LPUSH`는 token 1개만 허용합니다.
- `BRPOP`은 fixed key 1개만 허용합니다.
- wakeup list는 durable queue가 아니며 TTL을 가져야 합니다.

## 3. 조건부 허용 command

### LLEN

`LLEN` 자체는 O(1)이지만, busy polling을 유도하면 운영적으로 좋지 않습니다.

허용:

```text
low-frequency metric
admin exact-key diagnostic
```

금지:

```text
tight loop에서 LLEN으로 queue polling
```

### HGET/HSET single field

single field는 허용 가능합니다. 다만 hash 전체 cardinality가 커질 수 있으면 TTL 또는 bounded design이 필요합니다.

금지:

```text
HGETALL on unbounded hash
```

### SADD/SREM one member

single member 조작은 상황에 따라 허용 가능합니다. 단, 전체 set을 hot path에서 읽으면 안 됩니다.

금지:

```text
SMEMBERS on unbounded set
```

### ZADD/ZREM/ZCARD/ZRANGE LIMIT small

대부분 O(log N) 또는 cardinality에 의존합니다. strict O(1)은 아니므로 hot dispatch correctness path에서는 피합니다.

허용 조건:

1. cache/rate-limit/index path
2. key cardinality bounded 또는 TTL 존재
3. small LIMIT 강제
4. 코드 주석으로 예외 사유 명시

## 4. 금지 command

운영 hot path 금지:

```text
KEYS
SCAN without strict bounded non-hot path
LRANGE 0 -1
SMEMBERS unbounded
HGETALL unbounded
ZRANGE/ZREVRANGE without small LIMIT
SORT
SUNION/SINTER/SDIFF over unbounded sets
PUBLISH as alarm dispatch wakeup default
EVAL/EVALSHA with loops over variable-size data
```

## 5. Pub/Sub 정책

Pub/Sub는 alarm dispatch wakeup 기본 구현으로 사용하지 않습니다.

예외적으로 개발/로컬 디버깅 또는 작은 bounded subscriber 환경에서 쓸 수는 있지만, production 기본값은 fixed-list token 방식입니다.

예외 사용 시 코드 주석:

```go
// Complexity exception: Pub/Sub PUBLISH is not O(1) with respect to subscribers.
// This path is disabled in production alarm dispatch wakeup and exists only for local diagnostics.
```

## 6. Lua 정책

Lua script는 기본 금지입니다. 다만 loop 없는 single-token gate처럼 O(1) command만 조합하는 경우 예외적으로 허용할 수 있습니다.

허용 조건:

```text
- 반복문 없음
- variable-size data scan 없음
- key 개수 고정
- script 안의 command가 O(1) 계열
- 필요 사유 주석 존재
- fallback scan 존재
```

금지:

```text
- list 전체 순회
- set/hash 전체 순회
- pattern key scan
- 대량 delete
```

## 7. code review checklist

Valkey command가 추가되면 reviewer는 다음을 확인합니다.

```text
1. 이 command가 dispatch hot path인가?
2. command 복잡도는 무엇인가?
3. key cardinality가 bounded인가?
4. TTL이 있는가?
5. unbounded full read가 있는가?
6. fallback이 있는가?
7. 주석이 필요한 예외인가?
8. fake client/lint test로 금지 명령을 막는가?
```
