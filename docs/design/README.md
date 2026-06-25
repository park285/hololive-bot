# Design Docs

아직 구현 중이거나 제안 성격의 설계 문서를 둡니다.

## 포함 기준
- RFC
- 리팩터 설계
- 구현 전 design spec

## 연결 규칙
- 설계 문서는 승인 후 implementation plan으로 연결합니다.
- 구현이 끝나 현재 SSOT가 되면 `docs/current/`로 승격하거나 현재 문서에서 링크합니다.
- 폐기되면 `docs/history/`로 이동합니다.

## 현재 설계 문서 위치
- `docs/superpowers/specs/` — 현재 design spec 저장 위치
- `three-runtime-consolidation-plan.md` — `bot` + `admin-api` + `llm-scheduler`를 `hololive-api`로 통합해 3개 runtime으로 줄이는 migration plan

## Active Worklogs
- `2026-05-15-repo-structure-refactor-worklog.md` — repo structure refactor 완료 범위, 검증, 다음 작업 기준
- `2026-06-21-osaka-tiny-vps-runtime-handoff.md` — Osaka tiny VPS Docker Compose vs host-native `systemd` runtime 결정 handoff
