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
- `history/plan-kits/` - 더 이상 top-level entrypoint가 아닌 legacy plan-kit bundle

## Design

`docs/design`과 `docs/superpowers/specs`는 아직 current로 승격되지 않은 설계 제안을 둡니다.

- `design/README.md` - 설계 문서 규칙
- `design/three-runtime-consolidation-plan.md` - 5개 Go runtime을 3개 runtime으로 줄이기 위한 migration plan (완료 — 2026-06-27 cutover, `runbook_execution/THREE_RUNTIME_CUTOVER_20260627.md` 참조)
- `design/2026-05-15-repo-structure-refactor-worklog.md` - repo structure refactor worklog
- `design/repo-tree-classification.md` - completed top-level docs directory classification and move record
- `superpowers/specs/` - 승인된 design spec 보관 위치
- `superpowers/plans/` - 구현 plan 보관 위치

## Top-level 디렉터리 편입

3층에 속하지 않는 top-level 경로는 아래 결정(2026-07-17)에 따라 제자리에서 층을 배정합니다. 스크립트, `.github`, `docs/current` 문서가 경로를 참조하는 디렉터리는 링크 안정성을 위해 이동하지 않습니다. 모든 경로는 current 문서에서 발견 가능해야 하며, 새 SSOT로 확장하지 않습니다.

| 경로 | 층 | 결정 |
|---|---|---|
| `PROJECT_MAP.md` | current | `current/PROJECT_MAP.md`로의 compatibility bridge. 유지. |
| `architecture/` | current + history | gate asset(`*.txt` threshold/allowlist)은 current 층 — `scripts/architecture/*`가 경로를 참조하므로 이동 금지. 날짜 붙은 분석 `.md`는 5-runtime 시절 historical 기록이며 각 파일 상단 historical 배너로 구분. |
| `runbook_execution/` | current | 배포/릴리스 runbook 실행 기록. `.github/pull_request_template.md`, `scripts/architecture/render-release-notes.sh`, `current/DEPLOYMENT_BASELINE.md`가 참조하므로 유지. 새 운영 runbook은 `current/runbooks/`에 작성. |
| `superpowers/` | design + history | `specs/`는 design 층, `plans/`는 완료된 구현 plan 기록. `scripts/architecture/check-removed-runtime-regressions.sh`가 참조하므로 유지. |
| `agent-workflows/` | history + 진행 중 plan | agent 주도 plan/note 작업 공간. 완료된 plan/note는 history 층 기록으로 취급하되, `docs/current/` 문서들이 경로를 참조하므로 제자리 유지. |
| `handoff/` | history | 완료된 refactor handoff 기록(P1-P4 split, youtube-producer multi-worker). 새 handoff/closeout 문서는 `history/`에 작성. |
| `review/` | history | 날짜 붙은 review snapshot. 새 review 기록은 `history/`에 작성. |
