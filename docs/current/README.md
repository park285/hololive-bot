# Current Docs

현재 운영 기준 문서만 둡니다. 과거 완료 기록, handoff, closeout 문서는 `docs/history`로 이동하고 current에는 bridge만 남길 수 있습니다.

## Core

- `PROJECT_MAP.md` - 현재 module/runtime 운영 인벤토리
- `DEPLOYMENT_BASELINE.md` - Docker Compose 운영 baseline
- `SERVICE_OWNERSHIP.md` - 7개 runtime ownership 기준
- `CONTRACT_MAP.md` - 내부 HTTP/Queue/PubSub/external boundary 계약 지도
- `CONTRACT_MANIFEST.txt` - contract ID/provider/consumer/package/doc 검증 manifest
- `ERROR_CONTRACT.md` - 내부 API error response와 client 해석 규칙
- `QUEUE_AND_PUBSUB_CONTRACTS.md` - alarm queue와 settings Pub/Sub 계약

## Services

- `services/bot.md`
- `services/admin-api.md`
- `services/alarm-worker.md`
- `services/dispatcher-go.md`
- `services/llm-scheduler.md`
- `services/stream-ingester.md`
- `services/youtube-scraper.md`

## Contracts

- `contracts/README.md`
- `contracts/membernews.md`
- `contracts/majorevent.md`
- `contracts/trigger.md`
- `contracts/alarm.md`
- `contracts/settings.md`
- `contracts/iris-boundary.md`

## Runbooks

- `runbooks/README.md`

## Architecture And Governance

- `architecture/README.md`
- `architecture/ci-gates.md`
- `architecture/llm-work-rules.md`

## Compatibility Bridges

아래 파일은 기존 링크를 보존하기 위한 bridge입니다. current bridge는 짧은 이동 안내만 허용하며 historical 본문과 historical status body marker는 둘 수 없습니다.

- `ALARM_DISPATCH_REMEDIATION_20260414.md`
- `RUNTIME_SPLIT_HANDOFF_20260416.md`
- `RUNTIME_SPLIT_PR07_BLOCKERS_20260416.md`
- `CRITICAL_REVIEW_RESIDUAL_ISSUES_20260415.md`
- `REMAINING_RISKS_EXECUTION_GUIDE_20260415.md`
- `LEGACY_LINT_RESUME_20260415.md`
