## hololive-alarm-worker

- Files: go=44, test=19, ratio=30%
- Local guides: none

### LOC thresholds (Top 5)

1. `internal/service/alarm/checker/internal/checking/youtube_checker.go`: 447 / 480 (93%) — NEAR.
2. `internal/service/alarm/checker/internal/checking/notifier.go`: 408 / 460 (89%) — NEAR.
3. `internal/service/alarm/checker/internal/checking/chzzk_checker.go`: 395 / UNLISTED-LARGE.
4. `internal/app/internal/workerapp/alarm_dispatch_karing.go`: 393 / UNLISTED-LARGE.
5. `internal/service/alarm/checker/internal/checking/common.go`: 349 / UNLISTED-LARGE.

### Function budget (Top 5)

1. `internal/service/alarm/checker/internal/checking/youtube_checker.go:146` `collectDueYouTubeNotifications` ~27 lines (nested errgroup callbacks).
2. `internal/service/alarm/checker/internal/checking/notifier.go:206` `prepareOne` ~21 lines (multiple type assertion branches).
3. `internal/app/internal/workerapp/alarm_dispatch_runner.go:77` `dispatchGroup` ~50+ lines, cascading error handling.
4. `internal/app/internal/workerapp/build_runtime.go:184` `buildAlarmFoundation` ~56 lines, inline factory with heavy setup.
5. `internal/service/alarm/scheduler/runtime_scheduler.go:88` `NewRuntimeScheduler` ~55 lines, multi-dependency constructor.

### Test coverage gaps

1. `internal/app/internal/workerapp` 17 prod / 6 test (26%) — dispatch runners, maintenance, metrics, karing handlers mostly untested; `alarm_dispatch_runner_loop`, `alarm_dispatch_idle`, `alarm_dispatch_karing_community`, `build_egress` uncovered.
2. `internal/service/alarm/scheduler` 5 prod / 2 test (28%) — `runtime_scheduler_events`, `runtime_scheduler_cache_recovery`, `runtime_scheduler_twitch` untested.
3. `internal/service/alarm/checker/internal/checking` 15 prod / 8 test (34%) — `youtube_checker_inputs`, `youtube_checker_persisted_live`, `youtube_checker_stream_helpers`, `chzzk_checker_lookup_job`, `notifier_publish` untested.

### Naming inconsistencies

1. Package split: `checker/checker.go` re-exports types from `checker/internal/checking/*` via type aliases — `checker.YouTubeChecker` vs `checking.YouTubeChecker` ambiguity.
2. Interface name collision: `alarmDispatchSender` (workerapp:34) vs `alarmDispatchClientRequestSender` (:39) — `SendMessage` vs `SendKaringContentList` diverge rather than compose.
3. Platform-name casing drift: `ChzzkChecker`, `TwitchChecker`, `YouTubeChecker` mix conventions per platform.
4. Constant prefix redundancy: `alarmDispatchKaringMaxItemsPerRequest`, `alarmDispatchKaringDisplayLocation`, `alarmDispatchKaringTemplateIDByItemCount` (`alarm_dispatch_karing.go:16–24`) — repeated prefix in same file scope.
5. `checking/` package houses platform logic but external surface lives under `checker/`; intent unclear.

### Duplication / extraction candidates

1. Signal/shutdown handling: `internal/app/runtime/lifecycle.go:76–127` rebuilds graceful shutdown logic likely shared with bot-go and others.
2. Error-wrap pattern: `fmt.Errorf("operation: context details: %w", err)` 9+ occurrences in notifier.go, 13+ in alarm_dispatch_runner.go.
3. Dedup claim key lifecycle: `notifier.go:321–367` claim, `alarm_dispatch_runner.go:135` release — overlaps with YouTube outbox claim/release machinery.
4. Queue envelope consumer loop: `alarm_dispatch_runner.go:57–65` `DrainBatch → dispatch → MarkDispatched/ScheduleRetry/MoveToDLQ` mirrors `hololive-shared/pkg/service/alarm/queue/consumer.go`.
5. Valkey/cache bootstrap: `common.go:35` + `runtime_alarm_worker.go` + `build_runtime.go` — repeated factory pattern likely duplicated in hololive-shared.
6. HTTP server wrapper: `runtime/http_server.go:31–66` listen+shutdown — extract shared.
