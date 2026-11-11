# Current Docs

현재 운영 기준 문서만 둡니다. 과거 완료 기록, handoff, closeout 문서는 `docs/history`로 이동하고 current에는 bridge만 남길 수 있습니다.

## Core

- `PROJECT_MAP.md` - 현재 module/runtime 운영 인벤토리
- `CODEBASE_OVERVIEW.md` - 코드베이스 온보딩용 구조/흐름 요약
- `DEPLOYMENT_BASELINE.md` - 중앙·iris-seoul collector `b`·kapu collector `d`의 rootful Podman Quadlet과 Osaka collector `a` native systemd 운영 baseline
- `SERVICE_OWNERSHIP.md` - 3개 app runtime ownership 기준
- `CONTRACT_MAP.md` - 내부 HTTP·in-process 주입·PostgreSQL outbox·Pub/Sub·external boundary 계약 지도
- `ERROR_CONTRACT.md` - 내부 API error response와 client 해석 규칙
- `QUEUE_AND_PUBSUB_CONTRACTS.md` - alarm dispatch outbox(PostgreSQL)·delivery outbox Valkey wakeup 계약과 Pub/Sub 전달 의미 (settings Pub/Sub 제거, ACL은 in-process 전파)

## Services

- `services/hololive-api.md`
- `services/alarm-worker.md`
- `services/youtube-collector.md`
- `services/x-spaces.md`

## Contracts

- `contracts/README.md`
- `contracts/schedule-hololive-tv-api.md` - observed external schedule API usage map and reference
- `contracts/alarm.md`
- `contracts/settings.md`
- `contracts/admin-api.md`
- `contracts/shortlink.md`
- `contracts/iris-boundary.md`
- `contracts/source-observation-canonical-json-v1.md`
- `contracts/valkey_ephemeral_contract.md`

## Runbooks

- `runbooks/README.md`

## Architecture And Governance

- `architecture/README.md`
- `architecture/ci-gates.md`
- `architecture/llm-work-rules.md`
- `architecture/app-bootstrap-boundary-guide.md`
- `architecture/hololive-api-trust-domain.md`
- `architecture/non-secret-history-risk-decisions-20260713.md`
- `architecture/review-bundles.md`
- `architecture/youtube-egress-lifecycle-transition-ownership-20260831.md`
- `architecture/youtube-egress-lifecycle-contract-20260831.md`
- `architecture/youtube-egress-logical-delivery-ledger-20260831.md`
- `architecture/youtube-egress-lifecycle-commit-adjudication-20260831.md`
- `architecture/youtube-egress-lifecycle-library-review-20260831.md`
- [홀로봇 상세 리뷰와 리팩토링안](../history/architecture/2026-10-15-hololive-code-review-and-refactoring.md) - 관리자 API 결함 4건의 재현, 런타임별 구조 평가, 단계별 개선안과 검증 기록. 2026-10-15 리뷰 스냅샷이며 현재 계약이나 수정 완료 증명이 아닙니다.

## Active Implementation Plans

- `plans/2026-08-31-youtube-egress-lifecycle-implementation.md` - canonical identity, internal transition store, ledger backfill, writer/cleanup cutover와 검증 순서
- [상태머신·최소 한 번 처리 로직의 AP 이관 계획](plans/2026-10-19-ap-state-machine-consolidation.md) - Go/SQL 정책 중복, inbox/outbox·lease·재시도·복구, 동기 파생 모델의 단계별 이관안과 검증·승인 경계. #667로 main 병합·운영 적용 완료. [인계 기록](plans/2026-10-19-ap-state-machine-consolidation-handoff.md)에 단계별 검증·운영 적용·남은 위험이 있습니다.
- [YouTube 수집 경계 정리와 DB 보존 적체 해소 통합 계획](plans/2026-10-25-youtube-collection-and-retention-remediation.md) - collector Go·JS 경계 결함과 DB 보존 적체·보호 이력·인덱스 회수의 단계 순서, 의존, 검증·승인 경계. 세부 근거는 [DB 계획](plans/2026-10-25-db-growth-and-retention-remediation.md)과 [경계 리뷰 기록](../history/architecture/2026-10-25-youtube-collector-go-js-boundary-review.md)에 있습니다. 1~3단계 운영 적용, 4~6단계 미실행.
- [구조·성능·계약 정리와 호환 잔재 fadeout 리팩토링안](plans/2026-11-11-hololive-structure-fadeout-refactor.md) - 정본 문서 정정, 계약 위반 수정, 호출자 없는 호환 경로 삭제, 중복 구현 통합의 실행 WP 60개와 순서·범위 조정, 승인·운영 증거 대기 WP-62~74. 실행 WP 구현과 로컬 검증 완료, 미커밋.
