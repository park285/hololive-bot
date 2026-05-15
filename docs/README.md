# docs

저장소 문서는 `current / history / design` 세 층으로 관리합니다.

## Current

`docs/current`는 현재 운영 기준의 SSOT입니다. 운영자와 LLM은 먼저 current 문서를 읽고, history/design은 보조 근거로만 사용합니다.

- `current/README.md` - current 문서 인덱스
- `current/PROJECT_MAP.md` - 현재 module/runtime 운영 인벤토리
- `current/DEPLOYMENT_BASELINE.md` - Docker Compose 운영 baseline
- `current/SERVICE_OWNERSHIP.md` - runtime ownership map
- `current/CONTRACT_MAP.md` - 내부 계약 지도
- `current/ERROR_CONTRACT.md` - HTTP error response/client interpretation contract
- `current/QUEUE_AND_PUBSUB_CONTRACTS.md` - Valkey queue/PubSub contract
- `current/contracts/README.md` - 계약 문서 인덱스
- `current/runbooks/README.md` - runtime/infra runbook 인덱스
- `current/architecture/README.md` - current governance/architecture 인덱스

## History

`docs/history`는 완료된 migration, 과거 장애 대응, 더 이상 현재 기준이 아닌 handoff/closeout 문서를 둡니다.

- `history/README.md` - historical 문서 규칙과 인덱스

## Design

`docs/design`과 `docs/superpowers/specs`는 아직 current로 승격되지 않은 설계 제안을 둡니다.

- `design/README.md` - 설계 문서 규칙
- `design/2026-05-15-repo-structure-refactor-worklog.md` - repo structure refactor worklog
- `design/repo-tree-classification.md` - top-level docs directory classification before moves
- `superpowers/specs/` - 승인된 design spec 보관 위치
- `superpowers/plans/` - 구현 plan 보관 위치

## Legacy Bridges

아래 경로는 compatibility bridge 또는 legacy location입니다. current 문서에서 발견 가능해야 하며, 새 SSOT로 확장하지 않습니다.

- `PROJECT_MAP.md` - compatibility bridge to `current/PROJECT_MAP.md`
- `architecture/` - architecture gate asset location
- `runbook_execution/` - 기존 deployment/release runbook location
