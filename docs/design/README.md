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

- [Hololive 통합 리팩토링 계획](2026-10-02-hololive-api-refactoring.md) — API 동작·수명주기·성능 개선과 폴더·파일 개편을 합친 단일 실행 순서, shared/observation 이관, 경로·테스트 소비자 및 완료 기준
- [Hololive 폴더와 파일 경로 개편 심화 분석](2026-10-02-repository-layout-refactoring.md) — 통합 계획의 근거: Go 그래프 6개 조건, 임시 사본 이동 검증, Node 테스트·strict 검사 누락 재현, 파일별 이동표와 경로 소비자
- [YouTube 컬렉터와 공유 코드 분석 및 리팩토링안](2026-10-02-youtube-collector-shared-refactoring.md) — 미인식 upstream 오류의 INTERNAL 분류와 fatal 계약 복원, 탭 판정·재시도 경로 정리, 문자열 경계 테스트 삭제, collector 관점의 이관 보존 조건, 구현 순서와 실제 검증 결과
- [Hololive API 레거시 계층과 성능 부채 심층 분석](2026-10-02-hololive-api-deep-analysis.md) — 통합 계획의 근거: 설정 결과불명·종료 순서 재현, config/포트 설계, 병렬 SQL·CPU/할당 실험, observation 파일·SQL·테스트 이동표
- `docs/superpowers/specs/` — 현재 design spec 저장 위치
- `three-runtime-consolidation-plan.md` — `bot` + `admin-api` + `llm-scheduler`를 `hololive-api`로 통합해 3개 runtime으로 줄이는 migration plan

## Active Worklogs
- `2026-05-15-repo-structure-refactor-worklog.md` — repo structure refactor 완료 범위, 검증, 다음 작업 기준

종결된 Osaka runtime 선택 handoff는
`../history/runtime-split/2026-06-21-osaka-tiny-vps-runtime-handoff.md`에 보존하며,
현재 운영 경로는 `../current/runbooks/youtube-collector.md`가 소유합니다.
