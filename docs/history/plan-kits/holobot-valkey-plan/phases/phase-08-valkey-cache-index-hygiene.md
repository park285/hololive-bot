# Phase 08. Valkey cache/index hygiene와 command guard

## 목표

dispatch ledger를 PostgreSQL로 옮긴 뒤에도 Valkey는 cache와 재구성 가능한 index로 계속 사용될 수 있습니다. 이 phase는 Valkey 사용을 명확히 분류하고, O(1) command policy를 코드/테스트로 지키게 만드는 작업입니다.

## Valkey key 분류

### A. 순수 cache

예시:

```text
stream cache
channel info cache
profile translation cache
page cache
small readiness hint
```

요구사항:

- 사라져도 PG/API fallback 또는 재계산 가능
- TTL 있음
- correctness source가 아님

### B. PostgreSQL에서 재구성 가능한 index

예시:

```text
alarm subscriber index
member name index
room name index
channel subscriber mapping
```

요구사항:

- source of truth는 PG
- rebuild 가능
- rebuild 중 빈 cache가 dispatch 누락으로 이어지면 안 됨
- rebuild/admin path는 hot path와 분리

### C. dispatch 상태

예시:

```text
pending dispatch payload
retry schedule
DLQ
sent dedupe terminal state
```

요구사항:

- Valkey에 단독 저장 금지
- PostgreSQL ledger로 이동

## command guard

Valkey client wrapper 또는 fake client test에서 dispatch hot path command를 기록하고 allowlist를 검증합니다.

허용 예:

```text
SET exact key
GET exact key
EXISTS exact key
LPUSH exact key one value
BRPOP exactly one key
PEXPIRE exact key
TTL/PTTL exact key
UNLINK exact key in admin cleanup
```

금지 예:

```text
KEYS
SCAN in hot path
LRANGE 0 -1
SMEMBERS unbounded
HGETALL unbounded
ZRANGE without small LIMIT
PUBLISH alarm wakeup
EVAL with loop or variable-size scan
```

## O(log N) command 예외

일부 cache/index에서 sorted set이나 set command가 필요할 수 있습니다. 예를 들어 rate limiter나 subscriber index가 `ZADD`, `ZREM`, `ZCARD`를 쓸 수 있습니다.

이런 경우는 다음 조건을 만족해야 합니다.

1. dispatch correctness hot path가 아니어야 합니다.
2. key cardinality가 작거나 TTL로 bounded되어야 합니다.
3. 주석에 complexity와 bounded 근거를 남깁니다.
4. 조회는 small LIMIT을 강제합니다.
5. unbounded full materialization을 하지 않습니다.

예시 주석:

```go
// Complexity exception: ZADD is O(log N). This key is a per-room
// short-lived rate-limit bucket with TTL=60s and bounded member count.
// It is not part of the durable alarm dispatch path.
```

## subscriber index rebuild

rebuild 작업은 admin/background path입니다. 그래도 한 번에 모든 key를 지우거나 모든 subscriber를 가져오는 식은 피합니다.

권장:

- PG source table에서 keyset pagination
- Valkey write는 bounded batch
- 기존 index는 versioned key로 만들고 swap marker만 바꿈
- cleanup은 exact key 목록만 UNLINK

금지:

```text
KEYS alarm:channel_subscribers:*
SMEMBERS all subscribers on hot path
HGETALL all room names on hot path
```

## cache API 분리

Valkey client를 하나의 큰 interface로 두면 LLM/개발자가 hot path에서 금지 command를 쉽게 호출합니다. 역할별 interface를 분리하는 것이 좋습니다.

예시:

```go
type DispatchWakeupClient interface {
    NotifyDispatchAvailable(ctx context.Context) error
    WaitDispatchWakeup(ctx context.Context, timeout time.Duration) (bool, error)
}

type CacheClient interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type AdminIndexClient interface {
    RebuildSubscriberIndex(ctx context.Context, opts RebuildOptions) error
}
```

`DispatchWakeupClient`에는 `Keys`, `Scan`, `Publish`, `Range` 같은 메서드가 없어야 합니다.

## 테스트

필수 test:

1. dispatch wakeup client가 allowlist command만 호출
2. dispatch consumer wait가 BRPOP key 1개만 사용
3. code search/lint에서 `KEYS` hot path 사용 없음
4. Pub/Sub PUBLISH가 alarm dispatch wakeup에 쓰이지 않음
5. cache/index rebuild는 admin path로만 접근
6. O(log N) 예외에는 주석이 있음
7. unbounded LRANGE/SMEMBERS/HGETALL 호출 없음

## 완료 기준

- Valkey key 분류 문서화
- dispatch wakeup interface가 O(1) command만 노출
- 금지 command audit 통과
- 예외 command에는 근거 주석 존재
- cache/index rebuild가 hot path와 분리됨

## no-go 조건

- dispatch path에서 generic Valkey client를 그대로 노출
- alarm dispatch wakeup에 PUBLISH 사용
- 운영 코드에서 KEYS 사용
- room/member index 전체를 hot path에서 SMEMBERS/HGETALL로 읽음

## LLM 작업 프롬프트

```text
Valkey 사용을 cache/index/wakeup 역할별로 분리하고, dispatch hot path에서 O(1) command만 쓰도록 interface를 좁히세요.
DispatchWakeupClient에는 NotifyDispatchAvailable과 WaitDispatchWakeup 같은 메서드만 두고, KEYS/SCAN/PUBLISH/LRANGE 같은 메서드는 노출하지 마세요.
운영 hot path에서 KEYS, unbounded SCAN, LRANGE 0 -1, SMEMBERS, HGETALL, Pub/Sub PUBLISH wakeup을 제거하거나 금지하세요.
O(log N) command가 필요한 cache/index path는 hot path가 아님을 주석으로 설명하고, key cardinality 또는 TTL bound를 명시하세요.
```
