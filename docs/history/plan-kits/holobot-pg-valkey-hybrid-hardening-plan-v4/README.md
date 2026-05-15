# Holobot PG + Valkey Hybrid Dispatch Hardening Plan V4

이 문서 묶음은 현재 구현된 `pg_first/pg + Valkey wakeup` 경로를 “전환 가능한 1차 구현”에서 “고 fan-out production 기본 경로”로 올리기 위한 작업 계획입니다.

목표 모드는 다음입니다.

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
ALARM_DISPATCH_WAKEUP_ENABLED=true
```

핵심 결론은 단순합니다.

`pg_first/pg` 방향은 맞습니다. 그러나 현재 최근 구현은 `InsertBatch()` 내부가 아직 row-by-row 성격이고, dispatcher hot path에 reconciliation이 매번 끼며, Valkey가 PG fallback mode에서도 startup/readiness hard dependency로 남을 수 있습니다. 따라서 전체 전환 전에는 이 문서의 P0~P4를 먼저 끝내는 것이 안전합니다.

이 패키지는 LLM 작업에 넘기기 쉽도록 잘게 나누었습니다. 각 task card는 독립 작업 단위이며, 가능한 한 “작업 대상 파일”, “금지 사항”, “완료 기준”, “테스트”를 명확히 적었습니다.

권장 작업 순서는 다음입니다.

```text
P0  안전 게이트와 현재 구현 검증
P1  correctness hardening
P2  publisher 고 fan-out 성능 개선
P3  dispatcher hot path 성능/장애 개선
P4  schema/index/retention 정리
P5  metric/load test/canary 준비
P6  rollout/rollback
P7  Iris idempotency 후속 설계
```

가장 중요한 선행 작업은 세 가지입니다.

1. `dispatchoutbox.InsertBatch()`를 진짜 set-based multi-row insert로 바꾸기.
2. PG consumer mode에서 Valkey wakeup 장애가 dispatcher startup/readiness를 막지 않게 하기.
3. dispatcher의 reconciliation, retry, DLQ, quarantine 업데이트를 batch/throttle 구조로 바꾸기.

문서 색인:

```text
00-overview/      현재 구현 판정, 목표 아키텍처, 불변식
10-phases/        phase 단위 실행 계획
20-task-cards/    LLM에게 바로 줄 수 있는 작은 작업 카드
30-sql/           SQL template
40-prompts/       LLM 작업 프롬프트
50-checklists/    리뷰/수용/금지 체크리스트
60-runbooks/      canary, rollback, incident runbook
70-tests/         테스트 매트릭스와 부하 테스트 설계
80-reference/     소스 맵, 용어, metric catalog
```
