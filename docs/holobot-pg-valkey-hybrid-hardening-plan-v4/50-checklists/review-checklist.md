# Review Checklist

## Correctness

- [ ] PG가 dispatch source of truth입니다.
- [ ] Valkey wakeup token에 payload가 없습니다.
- [ ] wakeup 실패가 publish 실패가 아닙니다.
- [ ] shadowed row가 claim되지 않습니다.
- [ ] terminal row가 pending으로 복구되지 않습니다.
- [ ] stale leased는 retry입니다.
- [ ] stale sending은 Iris idempotency 전 quarantine입니다.
- [ ] MarkSending/MarkSent ownership 조건이 유지됩니다.

## Performance

- [ ] `InsertBatch()`가 event/delivery별 SQL 반복을 하지 않습니다.
- [ ] `ClaimDue()`는 bounded limit를 가집니다.
- [ ] `LoadEventsByID()`는 distinct event id만 로드합니다.
- [ ] reconciliation은 throttle됩니다.
- [ ] terminal/retry update는 batch입니다.
- [ ] retention은 chunked CTE입니다.

## Valkey

- [ ] `PUBLISH`를 dispatch wakeup에 쓰지 않습니다.
- [ ] `LPUSH`는 one token입니다.
- [ ] `BRPOP` key count는 1입니다.
- [ ] `KEYS`가 없습니다.
- [ ] unbounded `SCAN/LRANGE/SMEMBERS/HGETALL`이 없습니다.
- [ ] PG mode에서 Valkey wakeup 장애가 dispatcher를 멈추지 않습니다.

## Rollout

- [ ] forbidden mode pair가 막힙니다.
- [ ] legacy queue residue gate가 있습니다.
- [ ] canary metric이 있습니다.
- [ ] rollback runbook이 있습니다.
