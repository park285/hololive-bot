## hololive-youtube-producer

- Files: go=101, test=55, ratio=35%
- Local guides: none

### LOC thresholds (Top 5)

1. `internal/ops/communityshorts/internal/reports/community_shorts_alarm_sent_history_dataset_render.go`: 416 / 420 (98.1%) — NEAR.
2. `internal/ops/communityshorts/internal/reports/community_shorts_route_report_build.go`: 386 / UNLISTED-LARGE.
3. `internal/ops/communityshorts/internal/reports/community_shorts_latency_cause_build.go`: 380 / UNLISTED-LARGE.
4. `internal/runtime/ingestionlease/job_run_guard.go`: 377 / UNLISTED-LARGE — Valkey lease automation.
5. `internal/ops/communityshorts/internal/reports/community_shorts_delivery_logs_report.go`: 366 / UNLISTED-LARGE.

### Function budget (Top 5)

1. `internal/runtime/ingestionlease/lease.go:96` `StartRenewLoop` ~18 lines (ticker loop).
2. `internal/runtime/ingestionlease/lease.go:210` `withRetry` ~12 lines (exponential backoff harness).
3. `internal/runtime/ingestionlease/lease.go:166` `renew` ~33 lines (retry around CompareAndExpire).
4. `internal/runtime/leases/photo_sync_guard.go:43` `(leasedPhotoSyncService).Start` ~7 lines (acquire→runOwned loop).
5. `internal/runtime/readiness/readiness_recovery_loop.go:56` `runRecoveryLoop` ~30 lines (active-active recovery iteration with backoff).

Active-active key: `readiness_recovery_loop.go:87–108 recoveryLoopIteration` (~21 lines, TryClaim with backoff).

### Test coverage gaps

1. `internal/ops/communityshorts/internal/reports` 34 prod / 14 test (29%) — large builder/renderer chain (latency_cause, route_report, alarm_sent_history) lacks fixtures.
2. `internal/runtime/polltarget` 11 prod / 3 test (27%) — `youtube_poll_target_refresh.go` (348 lines) untested.
3. `cmd/ops/internal/communityshortscli` 10 prod / 4 test (29%) — latency_cause/latency_period parsers and flag handling untested.
4. `internal/ops/communityshorts` 1 prod / 0 test — aggregation/dispatcher untested.
5. `cmd/runtime/youtube-producer`, `cmd/ops/youtube-community-shorts` — no entry-point tests.

### Naming inconsistencies

1. `internal/runtime/readiness/ingestion_runtime_readiness.go:36–37` `ActiveActiveEnabled`/`ActiveActiveInstance` (PascalCase) vs `activeActive`/`instanceID` (camelCase) at :58–59.
2. Same file :65,119,178 — `"valkey_unavailable_active_active_fail_closed"` reason string mixes snake_case with PascalCase ActiveActive type.
3. `internal/runtime/ingestionlease/job_run_guard.go:69–85` Lua scripts call `redis.call()` despite codebase importing `valkey-go`.
4. `internal/ops/communityshorts/internal/reports` `OutboxCount` (field) vs `domain.OutboxKind` (enum) — singular/plural drift across report domains.
5. Two JobRunGuard implementations: `internal/runtime/polling/job_run_guard_claimer.go:14` wraps `internal/runtime/ingestionlease/job_run_guard.go:110` — naming pattern for poller-level vs lease-level coordination unclear.

### Duplication / extraction candidates

1. Ticker-based renewal loop repeated in `lease.go:106–113`, `photo_sync_guard.go:89–100`, `polltarget/youtube_poll_target_refresh.go:109–116`. Extract `runTickerLoop(ctx, interval, onTick)`.
2. Exponential backoff: `readiness_recovery_loop.go:141–153 nextRecoveryBackoff` and `lease.go:283–291 leaseBackoffDelay`. Extract `nextExponentialBackoff`.
3. Valkey/Redis lease scripts: `job_run_guard.go:69–108` Lua + `lease.go` SetNX/CompareAndExpire — extract `LeaseScripts`, `parseLeaseResult()`.
4. CLI slog setup repeated in `cmd/ops/internal/communityshortscli/{target_baseline.go:25,latency_period.go:34}`. Extract `newCLILogger(stderr)`.
5. Context-cancellation + done-channel pattern: `photo_sync_guard.go:72–80`, `readiness_recovery_loop.go:34–52`. Extract `runIsolatedLoop`.
6. Job identity constants — `readinessProbe{PollerName,ChannelID}` vs `photoSyncLease{PollerName,ChannelID}`. Extract `JobIdentityFor(service string)`.
