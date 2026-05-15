# Holobot Alarm Dispatch PG-First Logic Hardening Plan v2

이 문서 묶음은 `valkey_only → pg_first` 전환 이후의 알람 dispatch 경로를 재스캔하고, 현재 계약을 유지한 상태에서 로직만 고도화하기 위한 의사결정 문서입니다.

## 최종 결론

현재 계약은 유지합니다. 변경 대상은 runtime 로직, 실패 처리 정책, idle 대기 방식, 운영 자동화, 관측, 배포 절차입니다.

계약 유지 범위는 다음과 같습니다.

- `AlarmQueueEnvelope` JSON shape와 `QueueEnvelopeVersionV1` 유지.
- 기존 HTTP alarm API path 유지.
- 기존 dispatch queue key 유지.
- 기존 PG outbox table과 column 유지.
- 기존 dispatch mode 이름 유지: `valkey_only`, `shadow`, `pg_first`.
- 기존 consumer mode 이름 유지: `valkey`, `pg`.
- 기존 steady-state 조합 유지: `valkey_only/valkey`, `shadow/valkey`, `pg_first/pg`.
- `shadowed` row 자동 승격 금지.
- legacy Valkey residue를 PG로 replay 금지.
- Valkey wakeup은 payload delivery가 아니라 payload-free wakeup signal로만 사용.

## 문서 구조

| 파일 | 목적 |
|---|---|
| `00-decision-summary.md` | 전체 의사결정 요약 |
| `01-contract-freeze.md` | 변경하지 않을 계약과 변경 가능한 로직 범위 |
| `02-rescan-findings.md` | 재스캔 결과와 현 구조의 핵심 리스크 |
| `03-path-level-decision-matrix.md` | PATH 단위 의사결정 매트릭스 |
| `04-risk-register.md` | 리스크별 원인, 영향, 대응 |
| `05-acceptance-criteria.md` | 완료 판정 기준 |
| `10-phases/*` | 페이즈별 상세 계획 |
| `20-task-cards/*` | 구현 태스크 카드 |
| `30-appendix/*` | 환경변수, SQL, 알림룰, no-touch path |
| `40-review-gates/*` | 리뷰 체크리스트와 배포 게이트 |

## 페이즈 의존성

```text
Phase 00: Baseline gates
  ↓
Phase 01: PG failure semantics
  ↓
Phase 02: PG idle wakeup waiter
  ↓
Phase 03: PG consumer config parity
  ↓
Phase 04: Retention maintenance
  ↓
Phase 05: Observability and alerts
  ↓
Phase 06: Rollout and rollback
  ↓
Phase 07: Test matrix and verification
```

## 한 줄 원칙

PG는 durable ledger입니다. Valkey는 빠른 wakeup과 ephemeral cache입니다. 외부 전송 이후 실패는 retry가 아니라 quarantine입니다. 계약은 바꾸지 않고, 기존 계약을 더 엄격히 지키도록 runtime 로직을 보강합니다.
