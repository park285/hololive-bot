# Stream Ingester Structure Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize `hololive/hololive-stream-ingester` so runtime assembly and operator/reporting code no longer live in one catch-all package, while keeping the current behavior and binary names intact.

**Architecture:** Keep runtime behavior intact but move ownership boundaries into `internal/runtime` and `internal/ops`, then update command entrypoints to import the new packages. Keep runtime and YouTube scheduler wiring together in the runtime package for now, and split the largest ops dataset file into smaller same-package files so the ops package is not just a renamed dump.

**Tech Stack:** Go 1.26, monorepo workspace, Dockerfiles, `go test`, `go build`

---

### Task 1: Reorganize Command Entrypoints

**Files:**
- Create: `hololive/hololive-stream-ingester/cmd/runtime/stream-ingester/main.go`
- Create: `hololive/hololive-stream-ingester/cmd/runtime/youtube-scraper/main.go`
- Create: `hololive/hololive-stream-ingester/cmd/ops/*/main.go`
- Modify: `hololive/hololive-stream-ingester/Dockerfile`
- Modify: `hololive/hololive-stream-ingester/Dockerfile.youtube-scraper`
- Modify: `hololive/hololive-stream-ingester/Makefile`

- [ ] **Step 1: Move runtime binaries under `cmd/runtime`**

```go
import "github.com/kapu/hololive-stream-ingester/internal/runtime"

runtimeApp, err := runtime.BuildStreamIngesterRuntime(buildCtx, cfg, logger)
```

- [ ] **Step 2: Move report/dataset CLIs under `cmd/ops`**

```go
import opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"

report, err := opsapp.CollectCommunityAlarmSentHistoryReport(ctx, cfg, logger, now, opts)
```

- [ ] **Step 3: Update build targets**

```make
BINARY_NAME ?= stream-ingester

build-bin:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags go_json -trimpath -o bin/$(BINARY_NAME) ./cmd/runtime/$(BINARY_NAME)
```

- [ ] **Step 4: Update Dockerfiles**

```dockerfile
RUN go build -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/stream-ingester ./cmd/runtime/stream-ingester
```

- [ ] **Step 5: Verify runtime binaries still build**

```bash
go build ./hololive/hololive-stream-ingester/cmd/runtime/stream-ingester
go build ./hololive/hololive-stream-ingester/cmd/runtime/youtube-scraper
```

### Task 2: Move Runtime Ownership Into `internal/runtime`

**Files:**
- Create: `hololive/hololive-stream-ingester/internal/runtime/*.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/bootstrap.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/ingestion_runtime_readiness.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/stream_ingester_alarm_cache.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/stream_ingester_config_updates.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/stream_ingester_poller_registrations.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_lifecycle.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_runner.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/channel_target_validation.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_route_policy.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/youtube_poll_targets.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/youtube_poll_target_refresh.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/youtube_poll_target_refresh_metrics.go`
- Test: `hololive/hololive-stream-ingester/internal/app/*runtime*test.go`
- Test: `hololive/hololive-stream-ingester/internal/app/*poll*test.go`

- [ ] **Step 1: Create `internal/runtime` and move runtime files there**

```go
package runtime

func BuildStreamIngesterRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	return buildIngestionRuntime(ctx, cfg, logger, streamIngesterSpec(cfg))
}
```

- [ ] **Step 2: Export only the helpers ops code needs**

```go
type CommunityShortsBigBangPolicy = communityShortsBigBangPolicy

func BuildCommunityShortsBigBangPolicy(cfg config.IngestionConfig, channels []CommunityShortsOperationalChannel) (CommunityShortsBigBangPolicy, error)
```

- [ ] **Step 3: Update runtime tests and command imports**

```go
import runtimeapp "github.com/kapu/hololive-stream-ingester/internal/runtime"
```

- [ ] **Step 4: Verify runtime package**

```bash
go test ./hololive/hololive-stream-ingester/internal/runtime/...
go test ./hololive/hololive-stream-ingester/cmd/runtime/...
```

### Task 3: Move Reporting Ownership Into `internal/ops`

**Files:**
- Create: `hololive/hololive-stream-ingester/internal/ops/*.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_alarm_sent_history_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_alarm_sent_history_compare.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_alarm_sent_history_dataset.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_alarm_sent_history_finalizer.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_channel_summary_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_continuous_observation_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_delivery_logs_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_latency_cause_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_latency_period_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_observation_query_state.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_observation_window.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_route_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_send_counts_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_send_state_report.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/community_shorts_target_baseline.go`
- Move/Modify: `hololive/hololive-stream-ingester/internal/app/shorts_alarm_sent_history_report.go`

- [ ] **Step 1: Move report and dataset files into `internal/ops`**

```go
package ops

func CollectCommunityShortsSendStateReport(ctx context.Context, cfg *config.Config, logger *slog.Logger, now time.Time, options CommunityShortsSendStateCollectOptions) (CommunityShortsSendStateReport, error)
```

- [ ] **Step 2: Replace runtime-only imports with explicit runtime package imports**

```go
import runtimeapp "github.com/kapu/hololive-stream-ingester/internal/runtime"

policy, err := runtimeapp.BuildCommunityShortsBigBangPolicy(cfg.Ingestion, channels)
```

- [ ] **Step 3: Update all ops CLI entrypoints**

```go
import opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"

markdown := opsapp.RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report)
```

- [ ] **Step 4: Verify ops package and CLI entrypoints**

```bash
go test ./hololive/hololive-stream-ingester/internal/ops/...
go test ./hololive/hololive-stream-ingester/cmd/ops/...
```

### Task 4: Split the Largest Dataset File

**Files:**
- Create: `hololive/hololive-stream-ingester/internal/ops/community_shorts_alarm_sent_history_dataset_types.go`
- Create: `hololive/hololive-stream-ingester/internal/ops/community_shorts_alarm_sent_history_dataset_collect.go`
- Create: `hololive/hololive-stream-ingester/internal/ops/community_shorts_alarm_sent_history_dataset_render.go`
- Modify: `hololive/hololive-stream-ingester/internal/ops/community_shorts_alarm_sent_history_dataset.go`
- Modify: `hololive/hololive-stream-ingester/internal/ops/community_shorts_alarm_sent_history_dataset_test.go`

- [ ] **Step 1: Move public model types into a dedicated file**

```go
type CommunityShortsAlarmSentHistoryDatasetReport struct {
	GeneratedAt      time.Time
	Query            CommunityShortsAlarmSentHistoryDatasetQuery
	Summary          CommunityShortsAlarmSentHistoryDatasetSummary
	Results          CommunityShortsAlarmSentHistoryDatasetResults
}
```

- [ ] **Step 2: Move collection and normalization into a collection file**

```go
func CollectCommunityShortsAlarmSentHistoryDatasetReport(ctx context.Context, cfg *config.Config, logger *slog.Logger, now time.Time, options CommunityShortsAlarmSentHistoryDatasetCollectOptions) (CommunityShortsAlarmSentHistoryDatasetReport, error)
```

- [ ] **Step 3: Move markdown rendering into a render file**

```go
func RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report CommunityShortsAlarmSentHistoryDatasetReport) string
```

- [ ] **Step 4: Verify the dataset command and tests**

```bash
go test ./hololive/hololive-stream-ingester/internal/ops/... -run TestCommunityShortsAlarmSentHistoryDataset -count=1
go test ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts-alarm-sent-history-dataset -count=1
```

### Task 5: Final Verification And Cleanup

**Files:**
- Modify: `hololive/hololive-stream-ingester/Makefile`
- Modify: `hololive/hololive-stream-ingester/Dockerfile`
- Modify: `hololive/hololive-stream-ingester/Dockerfile.youtube-scraper`
- Remove: empty leftovers under `hololive/hololive-stream-ingester/internal/app`

- [ ] **Step 1: Remove or reduce `internal/app` so it no longer owns mixed concerns**

```bash
find hololive/hololive-stream-ingester/internal/app -maxdepth 1 -type f | sort
```

- [ ] **Step 2: Run package-scoped verification**

```bash
go test ./hololive/hololive-stream-ingester/...
go build ./hololive/hololive-stream-ingester/...
```

- [ ] **Step 3: Review final ownership shape**

```bash
find hololive/hololive-stream-ingester/cmd -maxdepth 3 -type f | sort
find hololive/hololive-stream-ingester/internal -maxdepth 3 -type f | sort
```
