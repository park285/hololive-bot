# Iris Standardization Excluding Game-Bot Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `game-bot-go`를 제외한 Iris 사용 레포들의 client/webhook/preset/docs/deploy 계약을 표준화하고, `hololive-bot`의 shared-go 거버넌스를 실제 active source 기준으로 바로잡습니다.

**Architecture:** `iris-client-go`를 Iris SDK의 단일 진입점으로 유지하되, preset 계층을 확장해서 소비 레포가 공통 transport/webhook 기본값을 재사용하게 만듭니다. `hololive-bot`는 잘못된 로컬 `shared-go` 그림자를 기준으로 돌아가는 docs/scripts만 수정합니다. 구현은 `/home/kapu/gemini/go.work`를 활성 workspace로 사용하는 것을 기본 전제로 하며, 불가하면 `chat-bot-go-kakao`에 임시 local `replace`를 넣었다가 최종 검증 전에 제거합니다. 새 preset/helper는 legacy `github.com/park285/iris-client-go/client|webhook` 소비자와 `github.com/park285/iris-client-go/iris/*` 소비자 둘 다 단계적으로 수용해야 합니다. 이 계획의 배포 계약은 명시적으로 고정합니다: 비게임 소비 레포는 `IRIS_BASE_URL`을 compose 기본값 없이 env로 주입하고, inbound webhook 소비자는 `IRIS_WEBHOOK_TOKEN`을 별도 env로 주입합니다. 물리적인 shared-go 경로 승격/이동은 이 계획에서 하지 않습니다.

**Tech Stack:** Go 1.26, `iris-client-go`, net/http, Gin, Docker Compose, Valkey, slog, shell scripts, Markdown docs

---

### Task 1: Expand `iris-client-go` presets into reusable client and webhook defaults

**Files:**
- Verify/Modify: `/home/kapu/gemini/go.work`
- Modify if needed: `/home/kapu/gemini/chat-bot-go-kakao/go.mod`
- Modify: `/home/kapu/gemini/iris-client-go/iris/preset/preset.go`
- Modify if needed: `/home/kapu/gemini/iris-client-go/iris/webhook/webhook.go`
- Create: `/home/kapu/gemini/iris-client-go/iris/preset/preset_test.go`
- Modify: `/home/kapu/gemini/iris-client-go/README.md`

- [ ] Add a failing compile/verification step that proves consumer repos cannot safely adopt the new preset API unless the top-level workspace or temporary `replace` path is made explicit
- [ ] Write failing tests for reusable client preset composition and webhook preset composition so the current thin preset layer is proven insufficient
- [ ] Run the top-level workspace check plus targeted `iris-client-go` tests and confirm the new cases fail for the expected missing helper/reuse reason
- [ ] Implement minimal preset helpers for common client options and webhook options, keeping repo-specific override points explicit and preserving compatibility with both legacy and `iris/*` import surfaces
- [ ] Re-run the targeted tests until they pass, then run the broader `iris-client-go` package tests that cover preset-adjacent behavior
- [ ] Update `iris-client-go` README examples to use the new preset entry points instead of ad hoc option chains

### Task 2: Converge `chat-bot-go-kakao` and `hololive-bot` onto preset-based Iris construction

**Files:**
- Modify: `/home/kapu/gemini/chat-bot-go-kakao/internal/app/app.go`
- Modify: `/home/kapu/gemini/chat-bot-go-kakao/internal/app/app_test.go`
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/pkg/providers/infra_providers.go`
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_webhook_youtube.go`
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go/internal/app/bootstrap_guard_additional_test.go`
- Modify: `/home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_dependency_views_test.go`

- [ ] Write failing tests that assert both repos still preserve their repo-specific overrides while delegating shared Iris defaults to presets
- [ ] Run targeted tests in `chat-bot-go-kakao` and `hololive-bot` to verify the new cases fail against the current ad hoc wiring
- [ ] Replace duplicated `iris.With*` chains with preset usage plus the minimum repo-specific overrides that must remain local, choosing either import migration to `iris/*` or the compatibility path from Task 1 explicitly per repo
- [ ] Re-run the targeted tests until they pass, then run the broader app/bootstrap suites that cover Iris client and webhook initialization
- [ ] Remove or simplify any now-redundant local helper code/comments that duplicated SDK behavior

### Task 3: Fix `hololive-bot` shared-go governance to follow the real active source

**Files:**
- Modify: `/home/kapu/gemini/hololive-bot/README.md`
- Modify: `/home/kapu/gemini/hololive-bot/docs/PROJECT_MAP.md`
- Modify: `/home/kapu/gemini/hololive-bot/docs/architecture/shared-go-package-allowlist.txt`
- Modify: `/home/kapu/gemini/hololive-bot/scripts/architecture/check-shared-go-boundary.sh`
- Modify: `/home/kapu/gemini/hololive-bot/scripts/architecture/check-shared-go-packages.sh`
- Modify: `/home/kapu/gemini/hololive-bot/scripts/architecture/export-go-workspace-import-graph.sh`
- Modify: `/home/kapu/gemini/hololive-bot/build-all.sh`
- Modify: `/home/kapu/gemini/hololive-bot/scripts/deploy/compose-redeploy-service.sh`

- [ ] Add a failing verification step or script test that proves `hololive-bot` currently inspects the shadow `shared-go/` tree instead of the active workspace source
- [ ] Run the architecture/shared-go checks and capture the current false-green behavior before changing them
- [ ] Implement path selection based on the active workspace/shared-go source while keeping the change localized to docs/scripts and explicitly not moving directories yet
- [ ] Re-run the shared-go boundary/package/import-graph checks and confirm they now inspect the active source rather than the local shadow copy
- [ ] Update the README and project map so build/test commands and module descriptions no longer imply that `./shared-go/...` is part of the active root workspace

### Task 4: Normalize cross-repo Iris route inventory and deployment contract

**Files:**
- Modify: `/home/kapu/gemini/Iris/README.md`
- Modify: `/home/kapu/gemini/chat-bot-go-kakao/README.md`
- Modify: `/home/kapu/gemini/chat-bot-go-kakao/.env.example`
- Modify: `/home/kapu/gemini/hololive-bot/docker-compose.prod.yml`
- Modify any repo-local `.env.example` or deployment docs that define `IRIS_BASE_URL` defaults in the touched repos

- [ ] Add failing doc/config checks or at least explicit verification steps that show the current webhook consumer inventory and `IRIS_BASE_URL` defaults are inconsistent
- [ ] Lock the deployment contract to explicit env injection: `IRIS_BASE_URL` required for all touched consumers, `IRIS_WEBHOOK_TOKEN` required for inbound webhook consumers, and record that choice in the touched docs
- [ ] Implement the minimal compose/doc changes to make that contract explicit and keep sensitive token handling unchanged
- [ ] Run compose config validation and doc grep checks to confirm the updated consumer inventory and `IRIS_BASE_URL` contract are consistent
- [ ] Remove stale examples that mention only a subset of real webhook consumers

### Task 5: Cross-repo verification and reconciliation

**Files:**
- Verify only

- [ ] Run targeted `go test` commands in `/home/kapu/gemini/iris-client-go`, `/home/kapu/gemini/chat-bot-go-kakao`, and `/home/kapu/gemini/hololive-bot` for the changed areas
- [ ] Run the architecture/shared-go scripts in `/home/kapu/gemini/hololive-bot` against the active shared-go source and confirm their output is meaningful
- [ ] Run `docker compose config` for the touched compose files to confirm syntax and variable wiring stay valid
- [ ] Reconcile the final diff against the design doc to verify `game-bot-go`, full shared-go relocation, and unrelated `llm` runtime changes remained out of scope
- [ ] Write a short follow-up note listing any remaining work that still requires a later top-level shared-go relocation or broader Iris topology migration
