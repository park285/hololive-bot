# Remove Dispatcher-Go Implementation Plan

> **For Codex agents:** Use `subagent-driven-development` only when the user authorized subagents; otherwise use `executing-plans` or implement directly with `update_plan`. Steps use checkbox syntax for tracking.

**Goal:** Remove the standalone `dispatcher-go` runtime, its rollback/legacy compose profile, and every active source-controlled connection point so notification egress is owned by `hololive-alarm-worker` only.

**Architecture:** Delete the `hololive/hololive-dispatcher-go` Go module and remove the service from Docker Compose, deployment scripts, smoke scripts, CI gates, workspace manifests, and active documentation. Keep generic dispatcher concepts that belong to current shared outbox or in-process alarm-worker logic, but remove standalone identifiers such as `dispatcher-go`, `hololive-dispatcher-go`, `legacy-dispatcher-go`, `smoke-dispatcher`, `DISPATCHER_PORT`, and port `30020`.

**Tech Stack:** Go workspace, Docker Compose, Bash deployment scripts, architecture gate scripts, Markdown runbooks.

---

## Removal Checklist

### Runtime Code And Workspace

- [x] Delete `hololive/hololive-dispatcher-go/`.
- [x] Remove `./hololive/hololive-dispatcher-go` from `go.work`.
- [x] Refresh `go.work.sum` with `go work sync`.
- [x] Remove dispatcher package paths from local CI and workspace tests.
- [x] Remove dispatcher Docker build-context copies from remaining runtime Dockerfiles.
- [x] Remove dispatcher build target from `hololive/hololive-kakao-bot-go/Makefile`.

### Compose, Build, Deploy, And Rollback Surface

- [x] Remove `dispatcher-go` service from `docker-compose.prod.yml`.
- [x] Remove `dispatcher-go` remote cache entry from `docker-compose.remote-cache.yml`.
- [x] Remove `legacy-dispatcher-go` profile logic and post-deploy cleanup from `build-all.sh`.
- [x] Remove `dispatcher-go` target aliases and cleanup logic from `scripts/deploy/compose-redeploy-service.sh`.
- [x] Remove dispatcher aliases from `scripts/logs/logs.sh`, `scripts/logs/lib/query.sh`, and `scripts/logs/lib/stream.sh`.
- [x] Delete `scripts/smoke/smoke-dispatcher-ready.sh`.
- [x] Remove dispatcher health entry from `scripts/smoke/smoke-runtime-health.sh`.
- [x] Remove dispatcher rollback/release smoke instructions from active runbooks.

### CI And Architecture Gates

- [x] Remove dispatcher module from `.github/workflows/architecture-gates.yml`.
- [x] Remove dispatcher module from `.github/workflows/dependency-hygiene.yml`.
- [x] Remove dispatcher module from `scripts/ci/local-ci.sh`.
- [x] Remove dispatcher module from `scripts/refactor/test-non-admin-go.sh`.
- [x] Remove dispatcher module from `scripts/refactor/grep-sensitive-logs.sh`.
- [x] Remove dispatcher module from `scripts/architecture/export-go-workspace-import-graph.sh`.
- [x] Remove dispatcher import-boundary check from `scripts/architecture/check-repository-ownership.sh`.
- [x] Remove dispatcher runtime coverage expectation from `scripts/architecture/check-runbook-coverage.sh`.
- [x] Replace the notification egress gate's legacy-profile assertion with an absence assertion.
- [x] Remove dispatcher file LOC thresholds.
- [x] Update ownership allowlists/manifests so `alarm-worker` is the only alarm dispatch consumer.
- [x] Update `testdata/entrypoint_contracts.json`.
- [x] Update workspace tests that enumerate runtime modules or Dockerfiles.

### Active Documentation

- [x] Delete `docs/current/services/dispatcher-go.md`.
- [x] Delete `docs/current/runbooks/dispatcher-go.md`.
- [x] Remove dispatcher entries from active indexes and maps.
- [x] Update active ownership/contract docs to state `alarm-worker` owns proactive notification egress.
- [x] Remove rollback legacy profile references from active runbooks.
- [x] Update active README/deployment docs and health/log command examples.
- [x] Update local alarm notification docs that still describe final send via standalone `dispatcher-go`.

### Verification

- [x] `rg -n 'hololive-dispatcher-go|dispatcher-go|legacy-dispatcher-go|smoke-dispatcher|DISPATCHER_PORT|30020'` has no active hits outside preserved historical/archive artifacts.
- [x] `bash -n` passes for touched shell scripts.
- [x] `go test` passes for remaining workspace modules.
- [x] `./scripts/architecture/ci-boundary-gate.sh` passes.
- [x] `COMPOSE_ENV_FILE=/run/hololive-bot/env ./scripts/deploy/compose.sh -f docker-compose.prod.yml config --quiet` passes.
- [x] `git status --short` shows only scoped removal/update changes.

## Task 1: Source And Compose Removal

**Files:**
- Delete: `hololive/hololive-dispatcher-go/`
- Modify: `go.work`
- Modify: `docker-compose.prod.yml`
- Modify: `docker-compose.remote-cache.yml`
- Modify: remaining runtime Dockerfiles
- Modify: `hololive/hololive-kakao-bot-go/Makefile`

- [x] Remove the module and all compose build references.
- [x] Run `go work sync`.
- [x] Run `rg` for standalone dispatcher identifiers and continue until active code/config hits are gone.

## Task 2: Script And CI Cleanup

**Files:**
- Modify: `build-all.sh`
- Modify: `scripts/deploy/compose-redeploy-service.sh`
- Modify: `scripts/logs/**`
- Delete: `scripts/smoke/smoke-dispatcher-ready.sh`
- Modify: `scripts/smoke/smoke-runtime-health.sh`
- Modify: `scripts/ci/local-ci.sh`
- Modify: `scripts/refactor/*.sh`
- Modify: `scripts/architecture/*.sh`
- Modify: `.github/workflows/*.yml`
- Modify: `internal/workspace/*.go`
- Modify: `testdata/entrypoint_contracts.json`

- [x] Remove standalone dispatcher targets, module lists, and rollback cleanup logic.
- [x] Update architecture gates to fail if `dispatcher-go` returns to active compose/docs.
- [x] Run `bash -n` on touched shell scripts.

## Task 3: Active Docs Cleanup

**Files:**
- Delete: `docs/current/services/dispatcher-go.md`
- Delete: `docs/current/runbooks/dispatcher-go.md`
- Modify: active docs under `docs/current/`
- Modify: `README.md`
- Modify: `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
- Modify: `hololive/hololive-kakao-bot-go/README.md`
- Modify: `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`

- [x] Remove active service/runbook pages for standalone dispatcher.
- [x] Replace legacy-dispatcher wording with alarm-worker ownership wording.
- [x] Remove rollback profile and direct dispatcher health/log instructions.

## Task 4: Final Audit

**Commands:**
- `rg -n 'hololive-dispatcher-go|dispatcher-go|legacy-dispatcher-go|smoke-dispatcher|DISPATCHER_PORT|30020' --glob '!logs/**' --glob '!docs/history/**' --glob '!artifacts/**'`
- `bash -n build-all.sh scripts/deploy/compose-redeploy-service.sh scripts/logs/logs.sh scripts/logs/lib/query.sh scripts/logs/lib/stream.sh scripts/smoke/smoke-runtime-health.sh scripts/ci/local-ci.sh scripts/refactor/test-non-admin-go.sh scripts/refactor/grep-sensitive-logs.sh scripts/architecture/*.sh`
- `go test ./... ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...`
- `./scripts/architecture/ci-boundary-gate.sh`
- `COMPOSE_ENV_FILE=/run/hololive-bot/env ./scripts/deploy/compose.sh -f docker-compose.prod.yml config --quiet`

- [x] Confirm no active standalone dispatcher identifiers remain.
- [x] Confirm tests/gates cover the removed module list and new ownership state.
- [x] Document any intentionally preserved historical references if they remain under `docs/history/` or `artifacts/`.
