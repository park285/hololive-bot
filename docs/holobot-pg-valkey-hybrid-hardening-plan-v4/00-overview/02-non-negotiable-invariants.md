# 절대 불변식

## correctness 불변식

1. PG에 delivery row가 없으면 production dispatch 대상이 아닙니다.
2. Valkey token은 wakeup일 뿐이며, payload와 delivery 상태를 담지 않습니다.
3. Valkey wakeup 실패는 publish 실패가 아닙니다.
4. `shadowed` row는 자동 claim 대상이 아닙니다.
5. `sent`, `dlq`, `quarantined`, `cancelled`는 terminal 상태입니다.
6. terminal 상태는 dedupe conflict 처리로 `pending`으로 돌아가면 안 됩니다.
7. `leased`는 외부 send 전 상태입니다. stale leased는 retry 가능합니다.
8. `sending`은 외부 send 결과가 불명확할 수 있는 상태입니다. Iris idempotency 전까지 stale sending은 quarantine입니다.
9. `MarkSent`는 `status='sending' AND locked_by=$workerID`에서만 성공해야 합니다.
10. `MarkSending`은 `status='leased' AND locked_by=$workerID`에서만 성공해야 합니다.

## performance 불변식

1. publisher hot path에서 notification 수만큼 SQL round-trip이 발생하면 안 됩니다.
2. dispatcher claim은 bounded `LIMIT`를 가져야 합니다.
3. event payload는 claim result에 delivery 수만큼 반복되어 반환되면 안 됩니다.
4. reconciliation은 bounded batch + throttle이어야 합니다.
5. retention delete/update는 반드시 chunked CTE여야 합니다.
6. 운영 dashboard는 고빈도 `COUNT(*)` scan에 의존하면 안 됩니다.

## Valkey complexity 불변식

1. hot path Valkey 명령은 allowlist 기반이어야 합니다.
2. `KEYS`는 금지입니다.
3. unbounded `SCAN`, `LRANGE 0 -1`, `SMEMBERS`, `HGETALL`은 hot path 금지입니다.
4. `BRPOP`은 wakeup key 1개만 넘깁니다.
5. `LPUSH`는 wakeup token 1개만 push합니다.
6. Pub/Sub `PUBLISH`는 기본 wakeup으로 쓰지 않습니다.
7. 고복잡도 명령이 필요한 경우, 코드 주석에 왜 필요한지와 bounded 조건을 써야 합니다.

## rollout 불변식

다음 조합은 steady state로 금지합니다.

```text
publisher=pg_first, consumer=valkey
publisher=valkey_only, consumer=pg
publisher=shadow, consumer=pg
```

전환 창에서는 alarm-worker와 dispatcher를 같은 배포 창에서 맞춰야 합니다.
