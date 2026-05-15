# Phase 03. Valkey O(1) wakeup 구현

## 목표

Valkey를 durable queue로 쓰지 않고, PostgreSQL delivery scan을 깨우는 latency helper로만 사용합니다. 사용자 제약에 따라 hot path는 O(1) 계열 명령 위주로 구현합니다.

기본 구현은 Pub/Sub가 아니라 단일 fixed list key wakeup입니다.

## 왜 Pub/Sub를 기본으로 쓰지 않는가

`PUBLISH`는 구독자 수와 pattern subscriber 수에 비례합니다. dispatcher replica 수가 작으면 실제 운영상 큰 문제는 아닐 수 있지만, strict O(1) 정책에는 맞지 않습니다.

따라서 V3 기본 wakeup은 다음 구조입니다.

```text
publisher: SET guard NX PX -> LPUSH one token -> PEXPIRE list
consumer : BRPOP one fixed key timeout
```

## key 설계

```text
wakeup list key  : alarm:dispatch:wakeup
wakeup guard key : alarm:dispatch:wakeup:guard
```

wakeup token 값은 payload가 아닙니다.

```text
value = "1"
```

dispatcher는 token 값을 신뢰하지 않습니다. token을 받으면 PostgreSQL `ClaimDue`를 실행할 뿐입니다.

## publisher wakeup algorithm

```text
1. PG transaction commit 성공
2. SET alarm:dispatch:wakeup:guard 1 NX PX 3000
3. SET 성공 시:
     LPUSH alarm:dispatch:wakeup 1
     PEXPIRE alarm:dispatch:wakeup 2500
4. SET 실패 시:
     wakeup suppressed metric 증가
5. 어떤 Valkey error도 publish 성공을 되돌리지 않음
```

중요한 점은 list TTL이 guard TTL보다 짧거나 같아야 한다는 것입니다. dispatcher가 죽어 있어도 token list가 무한히 쌓이는 것을 막습니다. wakeup은 durable queue가 아니므로 token이 빨리 사라져도 괜찮습니다.

권장 기본값:

```text
ALARM_DISPATCH_WAKEUP_GUARD_TTL_MS=3000
ALARM_DISPATCH_WAKEUP_LIST_TTL_MS=2500
ALARM_DISPATCH_WAKEUP_BRPOP_TIMEOUT_MS=1000
ALARM_DISPATCH_FALLBACK_SCAN_INTERVAL_MS=1000~5000
```

## consumer wait algorithm

```text
loop:
    processed = drainDueDeliveries(maxBatchesPerWake)
    if processed > 0:
        continue

    token = BRPOP alarm:dispatch:wakeup timeout
    if token received:
        continue

    fallback scan once
```

`BRPOP`에는 반드시 key를 하나만 넘깁니다. `BRPOP`의 복잡도는 key 개수에 비례하기 때문입니다.

## O(1) command allowlist

이 phase의 hot path에서 허용되는 Valkey command:

```text
SET exact-key value NX PX ttl
LPUSH exact-key one-token
PEXPIRE exact-key ttl
BRPOP one-fixed-key timeout
GET/EXISTS/TTL exact-key for readiness/diagnostic only
UNLINK exact-key for admin cleanup only
```

금지:

```text
PUBLISH alarm:dispatch:wakeup
KEYS alarm:*
SCAN over dispatch keys in hot path
LRANGE alarm:dispatch:wakeup 0 -1
LLEN 기반 busy polling
SMEMBERS/HGETALL of unbounded indexes
Lua script with loops
```

## token backlog에 대한 설명

위 알고리즘은 durable queue가 아니므로 token backlog를 보존하지 않습니다. 오히려 backlog가 쌓이지 않는 것이 목표입니다.

가능한 상황:

- publisher가 많은 알림을 commit함
- guard TTL 동안 wakeup은 한 번만 발생함
- dispatcher는 wakeup 후 PG에서 due delivery를 batch로 계속 drain함

즉 wakeup은 “몇 건이 들어왔다”가 아니라 “PG를 보라”는 신호입니다.

## fallback scan

Valkey wakeup이 유실될 수 있으므로 fallback scan이 필수입니다.

권장:

```text
ALARM_DISPATCH_FALLBACK_SCAN_INTERVAL_MS=1000~5000
```

fallback scan은 `ClaimDue(limit)` 한 번만 수행합니다. due row가 있으면 drain loop로 이어지고, 없으면 다시 wait합니다.

## 예외: Lua single-token gate

기본은 Lua 없이 구현합니다. 다만 운영에서 list token이 너무 자주 생기거나 race를 더 엄격히 제어해야 한다면, loop 없는 Lua script를 예외적으로 사용할 수 있습니다.

허용 조건:

1. script에 반복문이 없어야 합니다.
2. 접근 key는 wakeup list 1개와 guard 1개로 제한합니다.
3. 사용하는 command는 `EXISTS`, `LPUSH`, `PEXPIRE`, `SET`처럼 O(1)이어야 합니다.
4. 코드 주석에 “왜 Lua가 필요한지”를 남깁니다.
5. script SHA loading 실패 시 plain O(1) fallback 또는 fallback scan으로 복구됩니다.

하지만 이 예외는 기본 구현이 아닙니다.

## 테스트

필수 test:

1. PG commit 전 wakeup이 호출되지 않음
2. PG commit 후 wakeup 호출
3. guard set 성공 시 LPUSH 1회 + PEXPIRE 1회
4. guard set 실패 시 LPUSH 없음
5. wakeup 실패가 publish error로 전파되지 않음
6. consumer BRPOP 호출 key 개수가 정확히 1개
7. fake Valkey client에서 PUBLISH/KEYS/SCAN/LRANGE 0 -1 호출이 없음을 검증
8. wakeup이 없어도 fallback scan이 due delivery를 처리

## 완료 기준

- alarm dispatch wakeup에 Pub/Sub `PUBLISH`를 기본으로 쓰지 않음
- hot path Valkey command가 allowlist 안에 있음
- wakeup token에 payload가 없음
- wakeup 실패가 유실을 만들지 않음
- fallback scan이 동작함

## no-go 조건

- wakeup token에 payload, delivery id list, event key list를 넣음
- BRPOP에 여러 key를 넘김
- wakeup list를 durable queue처럼 drain함
- `PUBLISH`를 기본 wakeup으로 사용함
- dispatcher가 Valkey token만 보고 발송함

## LLM 작업 프롬프트

```text
alarm dispatch wakeup을 Valkey Pub/Sub가 아니라 O(1) fixed-list token 방식으로 구현하세요.
PG commit 이후에만 wakeup을 보냅니다.
SET guard NX PX 성공 시 LPUSH token 1개와 PEXPIRE를 호출합니다.
list TTL은 guard TTL보다 짧거나 같게 하여 dispatcher down 중 token backlog가 무한히 쌓이지 않게 하세요.
consumer는 BRPOP에 fixed key 1개만 넘깁니다.
Valkey token은 payload가 아니며, dispatcher는 token을 받으면 PG ClaimDue를 실행합니다.
PUBLISH, KEYS, unbounded SCAN, LRANGE 0 -1은 dispatch hot path에서 사용하지 마세요.
```
