# Review Checklist

## Contract

- [ ] Envelope schema 변경 없음.
- [ ] Queue version 변경 없음.
- [ ] Queue key 변경 없음.
- [ ] PG schema 변경 없음.
- [ ] Mode name 변경 없음.
- [ ] forbidden mode pair 허용 없음.

## Correctness

- [ ] Pre-send failure와 post-send failure가 분리됨.
- [ ] PG post-send failure는 quarantine.
- [ ] MarkSent failure는 retry하지 않음.
- [ ] Valkey legacy retry behavior는 유지됨.
- [ ] `pending`/`retry`만 claim됨.
- [ ] `sending` stale recovery가 quarantine으로 이어짐.

## Performance

- [ ] alarm-worker PG idle path에 25ms fixed polling 없음.
- [ ] wakeup wait가 기존 `alarm:dispatch:wakeup` key 사용.
- [ ] wakeup loss fallback polling 존재.
- [ ] maxBatchesPerWake 존재.
- [ ] DB pool budget 명시.

## Operations

- [ ] retention 자동화 또는 운영 job 존재.
- [ ] retention advisory lock 또는 단일 실행 보장.
- [ ] retention limit과 timeout 존재.
- [ ] rollback path가 stranded PG row를 다룸.
- [ ] manual requeue는 duplicate-risk ack 필요.

## Observability

- [ ] backlog oldest age metric 존재.
- [ ] wakeup timeout/failure metric 존재.
- [ ] post-send quarantine metric 존재.
- [ ] retention result metric 존재.
- [ ] alert rule 문서화.

## Tests

- [ ] unit tests.
- [ ] integration tests.
- [ ] wakeup disabled scenario.
- [ ] Iris timeout scenario.
- [ ] worker restart scenario.
- [ ] retention scenario.
