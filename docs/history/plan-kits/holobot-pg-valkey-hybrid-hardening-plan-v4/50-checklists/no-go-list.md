# No-Go List

다음 항목이 하나라도 있으면 전체 rollout을 중단합니다.

- `InsertBatch()`가 delivery별 insert 루프를 유지합니다.
- PG mode에서 Valkey 장애 시 dispatcher가 기동하지 못합니다.
- stale sending을 retry하도록 바꿨습니다.
- shadowed row를 자동 pending 승격합니다.
- Valkey wakeup에 payload를 넣습니다.
- `PUBLISH`를 기본 wakeup으로 사용합니다.
- `KEYS` 또는 unbounded scan/range가 hot path에 있습니다.
- terminal row를 자동 requeue합니다.
- retention SQL이 unbounded delete/update입니다.
- canary 전 legacy queue residue 확인이 없습니다.
