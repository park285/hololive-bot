# Repo Structure Refactor Worklog

## Purpose

이 문서는 2026-05-15 기준 repository structure refactor의 실제 완료 범위와 검증 증거를 기록한다. 현재 운영 SSOT가 아니라, 완료 audit용 design/worklog 문서다.

## Current Baseline

현재 기준 커밋:

- `ca7a3533 docs(architecture): define repo tree policy`
- `032159c4 fix(shared): preserve original youtube publish timestamps`
- `3b3526c9 docs(refactor): record repo structure worklog`

현재 `go.work` module 경계와 Docker Compose runtime 경계는 유지한다. 이번 단계에서는 Go module path, runtime binary path, Docker build target을 바꾸지 않았다.

## Completed Slice 1: Repo Tree Governance

완료한 일:

- `docs/current/architecture/repo-tree-policy.md` 추가
- `docs/current/architecture/README.md`에서 tree policy 연결
- `docs/design/repo-tree-classification.md` 추가
- `docs/README.md`에서 docs classification 문서 연결
- `scripts/architecture/check-tracked-local-artifacts.sh` 강화

정책으로 고정한 내용:

- root는 repository entrypoint와 workspace-level contract 중심으로 유지한다.
- `docs/current/`, `docs/history/`, `docs/design/`, `docs/superpowers/specs/`, `docs/superpowers/plans/` 역할을 구분한다.
- `logs/`, `data/`, `backups/`, `.review-bundles/`, local `.env*`, key/pem/archive, non-example `runtime-config/` 파일은 tracked local artifact로 막는다.
- `artifacts/architecture/go-workspace-import-graph.txt`, `logs/.gitkeep`, `runtime-config/.gitkeep`, `runtime-config/README.md`, `runtime-config/*.example` 예외는 유지한다.

검증:

```bash
bash -n scripts/architecture/check-tracked-local-artifacts.sh
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-current-docs-no-historical-body.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-tracked-local-artifacts.sh
./scripts/architecture/ci-boundary-gate.sh
```

추가 negative test:

- 임시 `GIT_INDEX_FILE`로 `.env`, `.env.local`, `.env.*.local`, `logs/...`, `runtime-config/secret.env`를 강제 stage했다.
- `check-tracked-local-artifacts.sh`가 해당 파일들을 실패 처리하는 것을 확인했다.
- 임시 파일은 제거했다.

## Completed Slice 2: SHARED Timestamp Preservation

완료한 일:

- `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch_writes.go`
  - existing `published_at`이 있으면 later scrape value로 덮어쓰지 않도록 변경
- `hololive/hololive-shared/pkg/service/youtube/tracking/*`
  - existing `actual_published_at`이 있으면 later value로 덮어쓰지 않도록 변경
- 회귀 테스트 추가
  - video upsert published_at 보존
  - community post upsert published_at 보존
  - content tracking actual_published_at 보존
  - alarm state actual_published_at 보존
  - source posts actual_published_at 보존

검증:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller ./hololive/hololive-shared/pkg/service/youtube/tracking
go test ./hololive/hololive-shared/...
```

## Completed Slice 3: Legacy Plan-Kit Relocation

완료한 일:

- legacy plan-kit bundle을 top-level `docs/`에서 `docs/history/plan-kits/`로 이동
  - `docs/history/plan-kits/holobot-pg-valkey-hybrid-hardening-plan-v4/`
  - `docs/history/plan-kits/holobot-pg-first-logic-hardening-plan-v2/`
  - `docs/history/plan-kits/holobot-valkey-plan/`
  - `docs/history/plan-kits/hololive-bot-baseline-bigbang-llm-docs-v8/`
  - `docs/history/plan-kits/hololive-bot-integrated-refactor-v3/`
  - `docs/history/plan-kits/hololive-main-server-logs-mirror-v2/`
  - `docs/history/plan-kits/hololive_scraper_plan_v2/`
- `docs/history/plan-kits/README.md` 추가
- `docs/README.md`, `docs/history/README.md`, `docs/design/repo-tree-classification.md`, `docs/current/architecture/repo-tree-policy.md` 갱신
- `scripts/refactor/test-validate-no-admin-touch.sh`의 moved plan-kit script reference 갱신
- moved `hololive-main-server-logs-mirror-v2` verifier가 새 historical plan-kit 위치에서 자체 artifact를 검증하도록 repo root/path 계산 갱신
- `scripts/architecture/check-docs-plan-kit-location.sh` 추가
- `scripts/architecture/ci-boundary-gate.sh`에서 legacy plan-kit location gate 실행

검증:

```bash
bash scripts/architecture/check-docs-plan-kit-location.sh
rg -n "docs/(holobot-pg-valkey-hybrid-hardening-plan-v4|holobot-valkey-plan|hololive-bot-baseline-bigbang-llm-docs-v8|hololive-bot-integrated-refactor-v3|hololive-main-server-logs-mirror-v2|hololive_scraper_plan_v2)" docs scripts -g '*.md' -g '*.sh' -g '*.py' -g '*.json'
bash scripts/refactor/test-validate-no-admin-touch.sh
bash docs/history/plan-kits/hololive-main-server-logs-mirror-v2/scripts/verify-main-log-mirror-v2.sh
```

추가 negative test:

- `check-docs-plan-kit-location.sh`를 먼저 추가한 뒤 top-level legacy plan-kit이 남아 있는 상태에서 실행했다.
- check가 legacy plan-kit top-level directory를 실패 처리하는 것을 확인했다.
- directory move와 reference update 뒤 같은 check가 통과했다.

## Completion Audit

이 worklog의 미완료 축은 아래 기준으로 닫혔다.

1. `hololive-stream-ingester` 내부 구조 cleanup
   - Evidence: `cmd/runtime`, `cmd/ops`, `internal/runtime/*`, `internal/ops/communityshorts/*`로 runtime/ops 경계가 분리되어 있다.
   - Verification: `go test ./hololive/hololive-stream-ingester/...`

2. `hololive-shared` slimming
   - Evidence: stream-ingester 전용 `IngestionLease`를 `hololive-shared/pkg/providers`에서 `hololive-stream-ingester/internal/runtime/ingestionlease`로 이동해 shared provider surface를 줄였다. YouTube timestamp repository 변경은 `hololive-shared/pkg/service/youtube/poller`와 `hololive-shared/pkg/service/youtube/tracking`의 domain/service persistence 경계 안에 머물렀다.
   - Verification: `go test ./hololive/hololive-stream-ingester/internal/runtime/ingestionlease ./hololive/hololive-stream-ingester/internal/runtime`, `go test ./hololive/hololive-shared/pkg/providers ./hololive/hololive-shared/pkg/service/youtube/poller ./hololive/hololive-shared/pkg/service/youtube/tracking`, `go test ./hololive/hololive-shared/...`

3. `hololive-llm-sched` scheduler/runtime 구조 정리
   - Evidence: `internal/schedulerkit`는 major event scheduler와 member news scheduler가 공유하는 guarded lifecycle runtime으로 사용 중이며, duplicate lifecycle behavior는 `schedulerkit.Runtime` tests로 고정되어 있다.
   - Verification: `go test ./hololive/hololive-llm-sched/...`

4. bot command assembly 단순화
   - Evidence: command path는 `bot.CommandRouter` -> `command.Registry.Execute` -> command handler 구조이며, module-local `AGENTS.md`/`CONVENTIONS.md`의 parser/formatter/registry 경계를 따른다.
   - Verification: `go test ./hololive/hololive-kakao-bot-go/...`

5. docs plan-kit 정리
   - Evidence: legacy plan kits are under `docs/history/plan-kits/`; stale top-level path references were removed from `docs` and `scripts`.
   - Verification: `bash scripts/architecture/check-docs-plan-kit-location.sh`, stale path `rg` returned no matches, `bash scripts/refactor/test-validate-no-admin-touch.sh`

6. repo-wide architecture gate
   - Evidence: `scripts/architecture/ci-boundary-gate.sh` now includes the legacy plan-kit location check.
   - Verification: `./scripts/architecture/ci-boundary-gate.sh`

7. repo-wide build/test
   - Evidence: Go workspace and Docker image build still pass after the `IngestionLease` package move and docs relocation.
   - Verification: `go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...`, `go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...`, `DB_PASSWORD=local-verification CACHE_PASSWORD=local-verification ADMIN_PASS_BCRYPT=local-verification SESSION_SECRET=local-verification IRIS_BOT_TOKEN=local-verification IRIS_WEBHOOK_TOKEN=local-verification ./build-all.sh --no-bump --build-only --skip-local-ci`

No production deploy or restart was performed in this worklog update.
