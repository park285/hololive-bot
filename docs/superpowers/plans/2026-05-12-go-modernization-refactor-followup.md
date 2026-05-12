# Go Modernization Refactor Follow-up Implementation Plan

> **For Codex agents:** Use `subagent-driven-development` only when the user authorized subagents; otherwise use `executing-plans` or implement directly with `update_plan`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clean up the lowest-risk refactor targets left after the Go runtime modernization pass and document larger follow-up work.

**Architecture:** Keep command-line healthcheck validation local but explicit. Make `automaxprocs` startup behavior a small decision object so logs and tests describe the same state machine.

**Tech Stack:** Go 1.26, `quic-go/http3`, `go.uber.org/automaxprocs`, monorepo `go.work`.

---

### Task 1: Harden Healthcheck URL Parsing

**Files:**
- Modify: `hololive/hololive-stream-ingester/cmd/runtime/healthcheck/main.go`
- Modify: `hololive/hololive-stream-ingester/cmd/runtime/healthcheck/main_test.go`

- [x] **Step 1: Add invalid URL tests**

Add table tests for missing scheme, unsupported scheme, and missing host. The expected behavior is a clear error before any HTTP request is attempted.

- [x] **Step 2: Verify the tests fail**

Run: `go test ./hololive/hololive-stream-ingester/cmd/runtime/healthcheck -run TestParseURLRejectsInvalidInputs -count=1`

Expected: fail against the current `url.Parse`-only implementation.

- [x] **Step 3: Add strict validation**

Require `http` or `https` scheme and a non-empty host in `parseURL`.

- [x] **Step 4: Validate**

Run: `go test ./hololive/hololive-stream-ingester/cmd/runtime/healthcheck -count=1`.

### Task 2: Make automaxprocs Startup Decision Explicit

**Files:**
- Modify: `shared-go/pkg/runtime/automaxprocs/automaxprocs.go`
- Modify: `shared-go/pkg/runtime/automaxprocs/automaxprocs_test.go`

- [x] **Step 1: Add decision tests**

Add tests for disabled, forced, and native-default skip decisions.

- [x] **Step 2: Verify the tests fail**

Run: `go test ./shared-go/pkg/runtime/automaxprocs -run TestAutomaxprocsDecision -count=1`

Expected: fail because the explicit decision helper does not exist yet.

- [x] **Step 3: Implement the decision helper**

Use a small typed decision value carrying `run`, `message`, and log fields. Keep existing environment variable behavior unchanged.

- [x] **Step 4: Validate**

Run: `go test ./shared-go/pkg/runtime/automaxprocs -count=1`.

### Follow-up Backlog

- `hololive/hololive-shared/pkg/config/config.go`: split `buildConfig` into `loadServerConfig`, `loadScraperConfig`, `loadWebhookConfig`, and service-specific loaders.
- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_cache_key.go`: change cache key construction to return `error` instead of silently using `"marshal-error"`.
- `hololive/hololive-shared/pkg/service/youtube/scraper/proxy_manager.go`: share URL/proxy validation helpers with other HTTP clients if more URL validation appears.
- `hololive/hololive-kakao-bot-go/internal/service/streamfeed/service.go`: extract Stellive/Chzzk merge orchestration from the service method after adding focused merge tests.
