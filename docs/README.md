# docs (Active)

완료된 실행/회고 문서는 정리하고, 현재 운영/진행에 필요한 문서만 유지합니다.

> 현재 운영 기준 (2026-03-07): k8s/k3s 기반 배포에서 Docker Compose 단일 호스트 운영으로 롤백했습니다. 운영 절차 SSOT는 compose 관련 문서입니다.

## 프로젝트 구조
- `PROJECT_MAP.md` — 모듈 인벤토리
- `20260306/CODEBASE_REFACTOR_AUDIT_20260306.md` — 현재 코드베이스 리팩토링 감사 보고서
- `20260306/CODEBASE_REFACTOR_TODO_20260306.md` — 실행용 리팩토링 TODO
- `20260306/AUTH_CORE_UNIFICATION_DRAFT_20260306.md` — auth core 단일화 초안
- `20260305/OUTBOX_MARKSENT_BATCH_PLAN_20260305.md` — outbox markSent 배치화 사전 설계
- `20260305/OUTBOX_PER_ROOM_DELIVERY_MODEL_PLAN_20260305.md` — outbox per-room 전달 정합성 개선안
- `20260307/COMPOSE_HISTORY_NOTES_20260307.md` — k8s→compose 회귀 기준 메모

## 정리/회고 문서
- `20260304/RUST_TO_GO_FULL_MIGRATION.md` — Go 단일 런타임 이관 완료 기록
- `20260306/CODEX_CLEANUP_REFACTOR_20260306.md` — 코드 정리 리팩토링 회고
- `20260306/CODEX_CLEANUP_SEPARATE_PR_20260306.md` — 분리 PR 전략 메모
- `20260306/PERF_IO_FALLBACK_REFACTOR_PLAN_20260306.md` — fallback/perf 리팩토링 계획

## 실행 문서
- `runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md` — 현재 운영 SSOT인 docker compose 배포 가이드
- `runbook_execution/YOUTUBE_SCRAPER_RUNBOOK.md` — `youtube-scraper` 전용 운영/장애 대응 런북
- `runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md`
- `runbook_execution/OUTBOX_PER_ROOM_ROLLOUT_RUNBOOK_20260305.md` — outbox per-room 모드 전환 런북
