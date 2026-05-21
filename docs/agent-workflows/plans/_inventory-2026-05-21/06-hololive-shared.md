## hololive-shared

- Files: go=658, test=215, ratio=24.6%
- Local guides: none at top level (sub-AGENTS scattered may exist; not enumerated at module root)

### LOC thresholds (Top 8)

1. `pkg/service/youtube/outbox/internal/delivery/dispatcher_send.go`: 611 / 980 (62%) — ample headroom.
2. `pkg/service/alarm/dispatchoutbox/repository.go`: 513 / 650 (78%).
3. `pkg/service/holodex/internal/holodexprovider/api_client.go`: 485 / UNLISTED-LARGE.
4. `pkg/service/notification/internal/alarmservice/alarm_cache.go`: 423 / UNLISTED-LARGE.
5. `pkg/service/notification/internal/alarmservice/alarm_service_mutation.go`: 421 / 650 (64%).
6. `pkg/service/youtube/outbox/internal/delivery/delivery_observation_context.go`: 409 / 420 (97%) — NEAR.
7. `pkg/service/youtube/stats/stats_repository_milestone.go`: 406 / UNLISTED-LARGE.
8. `pkg/config/internal/settings/config_types.go`: 396 / UNLISTED-LARGE.

Threshold-file drift: many listed ceilings are set far above current size (e.g., `dispatcher_send.go` 980 vs 611). Either the file shrank or the ceiling was set defensively. Worth re-baselining alongside Phase 2.

### Function budget (Top 8)

1. `pkg/service/youtube/outbox/internal/delivery/delivery_observation_context.go:243` `applyDeliveryTelemetryObservationContext` ~54 lines.
2. `pkg/service/youtube/outbox/internal/delivery/dispatcher_send.go:83` `(*Dispatcher).dispatchGroup` ~39 lines.
3. `pkg/service/youtube/outbox/internal/delivery/dispatcher_send.go:45` `(*Dispatcher).dispatchDeliveryRows` ~37 lines.
4. `pkg/service/youtube/outbox/internal/delivery/dispatcher_send.go:183` `(*Dispatcher).dispatchClaimedDeliveryRow` ~37 lines.
5. `pkg/service/alarm/dispatchoutbox/repository.go:469` `scanDeliveryRecord` ~29 lines (complex row mapping).
6. `pkg/service/youtube/outbox/internal/delivery/dispatcher_send.go:123` `(*Dispatcher).dispatchClaimedGroup` ~31 lines.
7. `pkg/service/youtube/outbox/internal/delivery/delivery_observation_context.go:379` ~31 lines (observation context assignment).
8. `pkg/service/youtube/outbox/internal/delivery/delivery_observation_context.go:348` ~30 lines (observation context matching).

All under 60-line ceiling, but `delivery_observation_context.go` compounds high nesting (~5 levels) with state-machine matching logic.

### Test coverage gaps

1. `pkg/service/youtube/poller/internal/polling` 38 prod / 12 test (24%) — `channel_stats_poller.go` (135), `community_shorts_detection.go` (68), `metrics.go` (214), `published_at_resolver_candidate.go`, `published_at_resolver_controls.go` untested.
2. `pkg/service/youtube/tracking/internal/observation` 20 prod / 6 test (23%) — `alarm_state_repository.go` (161), `*_claim.go`, `*_upsert.go`, `observation_compare_{index,mismatch,sort}.go`, `community_alarm_sent_history.go` (195) untested.
3. `pkg/service/alarm/queue` 7 prod / 2 test (22%) — `consumer_delayed_retry.go` (98), `consumer_payload.go`, `consumer_primitives.go`, `consumer_retry.go`, `metrics.go` (214) untested.
4. `pkg/service/cache` 12 prod / 6 test (33%) — mock client exists; valkey/Redis integration scenarios untested.
5. `pkg/service/notification/internal/alarmservice` 16 prod / 13 test (44%) — `alarm_service.go` (Get*) lacks unit tests; only `alarm_service_mutation.go` (421) is covered.

### Naming inconsistencies

1. Type-alias bridge layer: `pkg/service/youtube/outbox/outbox.go:5–42` re-exports 40+ internal types (`DeliveryRepository = delivery.DeliveryRepository`); readers must trace bridge to understand real type origin.
2. Receiver abbreviation drift: `alarm_service.go` uses `as *AlarmService`; elsewhere `svc`, `s`, `r` for similar receivers. No house style.
3. Interface vs struct casing for same concept: `pkg/service/delivery/dispatcher.go:45` `deliveryRepository interface` (unexported, lower) vs `pkg/service/youtube/outbox/internal/delivery/delivery_repository.go:36` `DeliveryRepository struct` (exported, Cap).
4. Compound package name: `pkg/service/alarm/dispatchoutbox/` — `dispatch` + `outbox` unclear as semantic unit; deepens with `outbox/internal/delivery/` layering.
5. Suffix convention drift: `AlarmService` (with Service) vs `Dispatcher` (no Service) vs `MessageSender` (no Service) — exported types pick suffix per file.
6. Repository file split style: member service `repository.{,mutation,query,scan}.go` (4 files); YouTube observation `repository.{,source_posts,identity,delivery_state}.go` (4 files). Concern-based splits avoid the `Repo` abbreviation trap but inflate file count.

### Duplication / extraction candidates

1. Claim + reuse cache: `pkg/service/youtube/outbox/internal/delivery/dispatcher_claim_gate.go:28–50` (`deliveryClaimToken`, `deliveryClaimReuseCache`) and `pkg/service/youtube/tracking/internal/observation/alarm_state_repository_claim.go:~30–80` repeat claim/reuse cache pattern. Consolidate.
2. Error-wrap + log: `logger.Error(...); return fmt.Errorf("operation: %w", err)` recurs ~1378 times. Extract `logAndWrapError(ctx, logger, op, err)`.
3. errgroup + SetLimit dispatch loops: `dispatcher_send.go:68–78` vs poller `scheduler_worker.go:~300–320`. Extract generic `parallelDispatch[T]`.
4. Row scan helpers: `dispatchoutbox/repository.go:469` and `youtube/stats/stats_repository.go` scan methods manually map `rows.Scan()`. Extract row mapper helper.
5. Observation window buffer: `delivery_observation_context.go:106–125` and `tracking/internal/observation/observation_window_repository.go` share window/retention machinery.
6. Type-alias re-export modules: `youtube/outbox/outbox.go:5–42` and analogous `tracking/tracking.go` re-exports — collapse into a single public surface package.
7. Retry policy: `alarm/queue/consumer_delayed_retry.go` (98) vs dispatcher retry in `outbox/internal/delivery/dispatcher_send.go`. Extract `RetryPolicy` + `executeWithRetry(ctx, policy, fn)`.
