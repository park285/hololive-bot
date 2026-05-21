## hololive-kakao-bot-go

- Files: go=125, test=62, ratio=33.1%
- Local guides: `hololive/hololive-kakao-bot-go/AGENTS.md`, `hololive/hololive-kakao-bot-go/CONVENTIONS.md`

### LOC thresholds (Top 5)

1. `internal/service/streamfeed/service.go`: 339 / UNLISTED-LARGE — merges live/upcoming streams with Stellive data, 20+ helper functions, mutex-guarded member cache.
2. `internal/bot/internal/orchestration/bot_transport.go`: 327 / UNLISTED-LARGE — 40+ functions handling Iris message/image/error sends with reply status polling, attempt retry, client request ID tracking.
3. `internal/adapter/internal/messaging/formatter_profile.go`: 324 / UNLISTED-LARGE — formatter with 15+ helper functions for profile sections.
4. `internal/command/internal/handlers/handler_live.go`: 306 / UNLISTED-LARGE — 17 functions across Live/Upcoming filters and Chzzk fallback logic.
5. `internal/adapter/internal/messaging/formatter_streams.go`: 302 / UNLISTED-LARGE — stream list formatter with 13+ functions; repetitive formatting patterns.

### Function budget (Top 5)

1. `internal/bot/internal/orchestration/bot_transport.go:91` `sendMessage` ~22 lines (nested error handling in attempt loop).
2. `internal/command/internal/handlers/handler_alarm.go:129` `handleAdd` ~50+ lines (deeply nested member resolution, alarm type parsing, graduation check, tier filtering).
3. `internal/adapter/internal/messaging/formatter_profile.go:117` `formatProfileDataEntries` ~30 lines, ~4-level nesting.
4. `internal/app/bootstrap/services.go:17` `InitBotInfrastructure` ~50+ lines, 6+ sequential defer/cleanup calls.
5. `internal/bot/internal/orchestration/bot_ingress.go:50+` ingress filter functions (ACL, self-sender, room filter); each 15–25 lines, collectively exceed budget.

### Test coverage gaps

1. `internal/adapter` (1 prod / 0 test) — adapter facade has zero coverage.
2. `internal/app/bootstrap` (15 prod / 1 test) — `InitBotInfrastructure`, alarm/YouTube stack wiring, member cache bootstrap untested.
3. `internal/app/http` (1 prod / 0 test) — webhook router has no tests.
4. `internal/command/internal/handlers` (20 prod / 8 test) — `handler_schedule`, `handler_subscriber`, `handler_stats`, `handler_help`, `handler_subscriber_graph`, member info group, news subscription untested.
5. `internal/bot/internal/orchestration` (22 prod / 11 test) — `bot_dependencies_validator`, `bot_message_error`, `bot_runtime_components`, `command_builder_clone`, `iris_client`, `log_attrs`, `log_events`, `thread_context` untested.

### Naming inconsistencies

1. Cache field naming drift: `cacheSvc` (param), `Cache` (struct field), `CacheSvc` (other struct) across `internal/app/bootstrap/{services_providers.go:23,types.go:93,services_modules.go:62}`.
2. Repository abbreviation mismatch: `internal/app/wiring/container.go:19` `MemberRepo()` returns `*member.Repository`; mixed `repo` vs `Repository` across provider functions.
3. Config abbreviation: `pgCfg` parameter for `config.PostgresConfig` in `internal/app/internal/botruntime/db_integration_runtime.go`.
4. Package-name vs type-name redundancy: `internal/service/matcher/matcher.go:72` `MemberMatcher` in package `matcher` produces `matcher.MemberMatcher`.
5. Plural/singular drift in message constants and formatter return types in `internal/adapter/internal/messaging/messages.go:245+`.

### Duplication / extraction candidates

1. HTTP server bootstrap and lifecycle wiring across `internal/app/runtime/http_server.go` and `internal/app/bootstrap/bot_server.go`; mirrored in admin-api, alarm-worker.
2. Member/Cache initialization: `internal/app/bootstrap/services_foundation.go:21` `ProvideMemberServiceAdapter()` and `providers_single_consumer.go:34` `ProvideMemberCacheWithoutValkey()` repeat shared provider invocations with module-specific layers.
3. Alarm stack initialization: `internal/app/bootstrap/services_alarm_stack.go:27` `InitAlarmYouTubeStack()` mirrors `hololive-shared/pkg/service/alarm/` consumer bootstrap.
4. Stream merge/filter helpers: `internal/service/streamfeed/service.go:137–270` (`mergeLiveStreams`, `mergeUpcomingStreams`) overlap with shared outbox/delivery filtering.
5. Signal/shutdown hook wiring: `internal/app/runtime/lifecycle.go:52–83` reassembles hooks that `shared-go/pkg/runtime/lifecycle` already provides.
6. Command error message formatting across `formatter_alarm.go`, `formatter_stats.go`, `formatter_major_event.go` — recurring `Err* → Korean message` translations.
