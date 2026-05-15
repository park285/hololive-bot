# T22. Chaos test scenarios

## 목적

장애 상황에서 유실/중복/무한 retry가 발생하지 않는지 확인합니다.

## 시나리오

1. Valkey wakeup unavailable.
2. Valkey wakeup token lost.
3. PG insert slow.
4. PG commit success + wakeup fail.
5. Iris timeout after MarkSending.
6. Iris success + MarkSent failure.
7. dispatcher crash after MarkSending before send.
8. dispatcher crash after send before MarkSent.

## 기대 결과

- Valkey 장애: PG fallback scan.
- wakeup fail: publish success, latency only.
- Iris ambiguous: quarantine.
- leased stale: retry.
- sending stale: quarantine.

## LLM 프롬프트

위 chaos scenario를 unit/integration test로 가능한 범위까지 구현하십시오. 외부 send 결과 불명은 retry가 아니라 quarantine이어야 합니다.
