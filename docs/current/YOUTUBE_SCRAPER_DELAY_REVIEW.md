# YouTube Scraper Delay Review

Last updated: 2026-04-11

## Summary

The dominant cause of delayed YouTube community/shorts alerts was not a single slow function. The primary issue was that `youtube-scraper` scheduled content pollers against the full operational member set while actual alert subscribers covered a much smaller channel set.

Observed runtime evidence before this change:

- subscriber cache warming reported `channels_loaded=12`
- scraper scheduler reported `active_members=111`, `total_jobs=555`
- scraper budget warning reported `expected_total_rpm=26.208333333333332`, `budget_rpm=20`

That mismatch meant the runtime was spending most of its YouTube HTML budget on non-alert channels, which directly increased publish-to-detect delay for the channels that actually mattered.

## Review provenance

This review consolidated externally proposed changes into repository-native code review and implementation work.
Local patch artifact file names are intentionally omitted here because they are not part of repository SSOT and may not exist in every workspace.

The reviewed change set contained four classes of change:

1. Per-poller channel targeting for the scraper scheduler
2. Outbox timing floor reduction
3. Scheduler dispatcher timer rewrite
4. Additional throughput work such as parallel subscriber lookup

## Applied in this pass

### 1. Per-poller scraper target sets

Applied.

The scheduler now accepts channel targets per poller registration instead of forcing a single global channel set across all pollers.

Implementation points:

- `hololive-shared/pkg/providers/scraper_scheduler_options.go`
- `hololive-shared/pkg/providers/youtube_providers.go`
- `hololive-stream-ingester/internal/app/stream_ingester_poller_registrations.go`
- `hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`
- `hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go`
- `hololive-stream-ingester/internal/app/youtube_poll_targets.go`

Behavior:

- `videos`, `shorts`, `community`, `live` use alarm-backed notification channels
- `channel_stats` keeps the broader operational channel set
- startup logs now emit:
  - `notification_target_channels`
  - `stats_target_channels`
  - `dropped_alarm_targets`
  - per-poller `target_channels`
  - recalculated `expected_total_rpm`

### 2. Outbox first-cycle timing floor

Applied.

The outbox dispatcher now runs `processOnce` immediately on startup and its default poll interval is reduced from `30s` to `2s`.

Implementation point:

- `hololive-shared/pkg/service/youtube/outbox/dispatcher.go`

This removes the avoidable cold-start wait and lowers steady-state enqueue-to-send delay from a worst-case 30 seconds to a much smaller polling window.

## Applied in the second pass

### 3. Scheduler timer rewrite

Applied.

The scraper scheduler no longer waits on a fixed 1 second dispatcher ticker. It now sleeps until the next due job and uses a short retry delay when the worker channel is full. A buffered wake channel is used so newly registered or rescheduled earlier work is not hidden behind an old timer deadline.

Implementation point:

- `hololive-shared/pkg/service/youtube/poller/scheduler.go`

### 4. Parallel subscriber lookup in outbox room collection

Applied.

`collectRoomsByChannel` now performs typed subscriber lookups in parallel and merges results after lookup completion, while preserving the distinction between:

- lookup failure: no map entry written
- empty subscriber set: empty room set written

Implementation point:

- `hololive-shared/pkg/service/youtube/outbox/dispatcher.go`

### 5. Telemetry loop separation

Applied.

Delivery telemetry flushing was removed from the outbox hot path and moved into a dedicated background loop. The outbox processing loop now focuses only on claim, fan-out, and send work.

Implementation points:

- `hololive-shared/pkg/service/youtube/outbox/dispatcher.go`
- `hololive-shared/pkg/service/youtube/outbox/dispatcher_telemetry.go`

### 6. Runtime poll target refresh

Applied.

The reviewer found that fixed poll targets at startup would leave newly added alarm channels unpolled until the next restart. A background refresher now uses the cache-backed alarm channel registry as the primary source of truth, with guarded DB fallback for cache-failure recovery and for sustained empty-cache recovery after the grace window, and synchronizes notification pollers in-place without recreating the runtime.

Implementation points:

- `hololive-shared/pkg/service/youtube/poller/scheduler.go`
- `hololive-shared/pkg/providers/scraper_scheduler_options.go`
- `hololive-stream-ingester/internal/app/youtube_poll_target_refresh.go`
- `hololive-stream-ingester/internal/app/stream_ingester_runtime_runner.go`
- `hololive-stream-ingester/internal/app/stream_ingester_runtime_lifecycle.go`

### 7. Telemetry cadence decoupling

Applied.

The reviewer also flagged that telemetry flushing was still tied to the 2 second outbox poll loop. Telemetry now uses its own `TelemetryPollInterval` with a default of 30 seconds, so the outbox fast path stays responsive without forcing the telemetry maintenance loop to hit the database every 2 seconds.

## Applied in the follow-up hardening pass

### 8. Subscriber cache warm on YouTube startup

Applied.

Subscriber cache warming is now tied to `youtubeEnabled` runtime startup instead of the `communityShortsBigBangEnabled` feature flag. This keeps outbox subscriber resolution from starting cold on normal YouTube runtime boot.

Implementation points:

- `hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go`
- `hololive-stream-ingester/internal/app/bootstrap_stream_ingester_warm_test.go`

### 9. Cache-safe subscriber lookup for outbox fan-out

Applied.

Outbox room resolution now uses the alarm seam's cache-aware subscriber resolver. If the typed cache key is empty or cache lookup fails, the resolver falls back to the DB-backed alarm set and only treats the channel as having no subscribers when both sources are empty. When DB fallback finds subscribers, the cache is rewarmed from the loaded alarm rows.

Implementation points:

- `hololive-shared/pkg/service/alarm/targets.go`
- `hololive-shared/pkg/service/youtube/outbox/dispatcher.go`
- `hololive-shared/pkg/service/alarm/targets_test.go`
- `hololive-shared/pkg/service/youtube/outbox/dispatcher_collect_rooms_parallel_test.go`

### 10. Poll target refresh safety and startup parity

Applied.

The runtime refresh path now keeps explicit target groups per poller registration, preserves explicit-empty notification target sets, skips scheduler sync when the resolved target set has not changed, keeps the previous resolved target set during transient empty-cache grace windows, and gives newly added notification targets an immediate first run without breaking steady-state anchor scheduling.

Implementation points:

- `hololive-shared/pkg/providers/scraper_scheduler_options.go`
- `hololive-stream-ingester/internal/app/stream_ingester_poller_registrations.go`
- `hololive-shared/pkg/service/youtube/poller/scheduler.go`
- `hololive-stream-ingester/internal/app/youtube_poll_target_refresh.go`
- related focused tests under `hololive-shared/pkg/service/youtube/poller` and `hololive-stream-ingester/internal/app`

## Expected post-deploy signals

After redeploying `youtube-scraper`, logs should show:

- `Resolved YouTube poll targets notification_target_channels=<small set> stats_target_channels=<broader set>`
- `Scraper poller targets resolved poller=videos|shorts|community|live target_channels=<notification set size>`
- `Scraper poller targets resolved poller=channel_stats target_channels=<stats set size>`
- `Scraper scheduler initialized ... total_jobs=<substantially smaller than 555>`
- `scraper_poll_budget_exceeds_rate_limit` should disappear
- repeated zero-work `Outbox per-room enqueue completed` info logs should not appear every 2 seconds

Observed after redeploy on 2026-04-11:

- `notification_target_channels=12`
- `stats_target_channels=111`
- `total_jobs=159`
- `expected_total_rpm=3.1083333333333334`
- `poll_interval=2s`
- `YouTube poll target refresher started`
- no repeated zero-work outbox enqueue info spam during short post-start observation

## Remaining work

No further remediation is required to close this delay incident.

Possible future optimizations still exist, but they are no longer part of the required remediation for this issue:

- published-at resolution decoupling for burst handling
- additional telemetry cadence tuning independent from outbox poll cadence
- further runtime observability cleanup

## Verification focus

Use these intervals after redeploy:

1. Compare `publish -> detect` and `detect -> sent` for the next 10 to 20 posts.
2. Confirm `notification_target_channels` is close to real subscriber coverage, not full active member count.
3. Confirm `expected_total_rpm` falls under the scraper budget.
4. Confirm outbox send latency is no longer dominated by the old 30 second polling floor.
