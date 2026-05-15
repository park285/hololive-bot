# T16. Blocking wakeup client 분리

## 목적

`BRPOP` blocking wait가 다른 Valkey command를 막지 않게 합니다.

## 작업 대상

- cache service 또는 dispatcher runtime
- wakeup wait tests

## 작업

1. PG dispatcher wakeup wait용 Valkey client/connection을 분리합니다.
2. `BRPOP`은 `alarm:dispatch:wakeup` key 하나만 사용합니다.
3. wakeup client failure는 PG fallback sleep으로 처리합니다.
4. wakeup list TTL은 guard TTL보다 길게 조정합니다.

## 권장 TTL

```text
wakeup guard TTL = 3s
wakeup list TTL  = 5~10s
```

## 완료 기준

- `BRPOP` 중 readiness/cache command가 block되지 않습니다.
- wakeup client nil/error 시 sleep fallback으로 동작합니다.
- BRPOP key count 1개 테스트가 있습니다.

## LLM 프롬프트

blocking Valkey wait가 다른 cache usage를 막지 않도록 전용 wakeup client 또는 전용 connection 경로를 구현하십시오.
