# docs

현재 운영/진행에 필요한 문서만 유지합니다. 완료된 문서는 `archived/`로 이동합니다.

> 현재 운영 기준 (2026-03-07): Docker Compose 단일 호스트 운영. SSOT는 compose 관련 문서.

## 프로젝트 구조
- `PROJECT_MAP.md` — 모듈 인벤토리

## 아키텍처
- `architecture/BOUNDARY_GATE_POLICY_20260303.md` — CI 경계 게이트 정책
- `architecture/IRIS_BOT_COMMUNICATION_ANALYSIS_20260312.md` — Iris ↔ Bot 통신/순서 보장 분석
- `architecture/*.txt` — CI 참조 기준값 (LOC 임계값, release 자산, shared-go allowlist)

## 활성 계획
- `superpowers/plans/PERF_IO_FALLBACK_REFACTOR_PLAN_20260306.md` — 성능/I/O/fallback 리팩토링 (P0 미완료)
- `superpowers/plans/2026-03-14-transport-layer-optimization.md` — Dispatcher↔Iris 트랜스포트 최적화
- `superpowers/specs/2026-03-17-settlement-bot-design.md` — 정산 봇 커맨드 설계

## 운영 런북
- `runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md` — Docker Compose 배포 SSOT
- `runbook_execution/YOUTUBE_SCRAPER_RUNBOOK.md` — YouTube Scraper 운영/장애 대응
- `runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md` — 릴리스 노트 템플릿

## 아카이브
- `archived/` — 완료된 설계/회고/TODO 문서
