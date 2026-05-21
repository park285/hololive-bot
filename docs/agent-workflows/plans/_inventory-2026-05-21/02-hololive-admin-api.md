## hololive-admin-api

- Files: go=35, test=22, ratio=62%
- Local guides: none

### LOC thresholds (Top 5)

1. `internal/server/internal/api/api_member.go`: 430 / 430 (100%) — OVER.
2. `internal/app/build_runtime.go`: 387 / 400 (97%) — UNLISTED-LARGE.
3. `internal/server/internal/api/settings_handler.go`: 363 / 400 (91%) — UNLISTED-LARGE.
4. `internal/server/internal/api/api_youtube_ops.go`: 347 / 400 (87%) — UNLISTED-LARGE.
5. `internal/server/internal/api/api_auth.go`: 318 / 400 (80%) — UNLISTED-LARGE.

### Function budget (Top 5)

1. `internal/app/build_runtime.go:55` `BuildAdminAPIRuntime` ~60 lines, 9 sequential setup steps with ~4-level nesting.
2. `internal/server/internal/api/api_member.go:314` `UpdateMemberName` ~58 lines.
3. `internal/server/internal/api/api_member.go:252` `UpdateChannelID` ~58 lines (mirror of UpdateMemberName).
4. `internal/service/system/stats.go:124` `collectCurrentStats` ~48 lines with 5 conditional branches.
5. `internal/server/internal/api/settings_handler.go:200` `UpdateSettings` ~24 lines, compound bind+validate+mutate+publish.

### Test coverage gaps

1. `internal/server/internal/api/` 17 prod / 14 test — `api_alarm`, `api_majorevent`, `api_deps`, `api_domains`, `oauth_proxy` lack dedicated tests; `api_low_coverage_test.go` is a 452-line catch-all.
2. `internal/app/` 7 prod / 3 test — `runtime_admin_api.go`, `*_shutdown.go`, `*_runner.go`, `runtime_build_inputs.go`, `settings_applier.go`, `http/{registration,middleware}.go` uncovered.
3. `internal/app/runtime/` 2 prod / 0 test — `lifecycle.go` (215), `http_server.go` (77) entirely untested.
4. `internal/service/system/` 1 prod / 2 test — Collector logic mixed with HTTP context in tests.
5. `internal/service/trigger/` minimal surface.

### Naming inconsistencies

1. Domain handler wrapper pattern inconsistent: `MemberAPIHandler`, `AlarmAPIHandler`, `RoomAPIHandler` embed `*APIHandler` anonymously; receiver names read domain-specific but are proxies (`api_domains.go:24–35`).
2. `internal/server/internal/api/api.go:55` `APIHandler` duplicates package name — `api.APIHandler` is awkward.
3. `internal/server/internal/api/api_auth.go:36` `AuthHandler` is direct (no wrapper); breaks pattern with other domain handlers.
4. Helper method casing/verb drift: `parsePositiveMemberID`, `bindAliasRequest`, `handleAliasOperation` — inconsistent prefix style (`api_member.go:49, 69, 114`).
5. `internal/server/internal/api/settings_handler.go:31` `SettingsHandler` exported but not wrapped — uniformity with domain group broken.

### Duplication / extraction candidates

1. `api_member.go:252–372` (`UpdateChannelID`/`UpdateMemberName`) — identical parse → bind → repo → cache refresh → activity log; extract update handler factory.
2. Error-response + log boilerplate: `h.safeLogger().Error(...); sharedserver.RespondError(c, 500, ..., nil)` appears 30+ times across `api_member`, `api_auth`, `api_room`, `api_profile`. Extract `respondWithError`.
3. `internal/app/build_runtime.go:55–114` `BuildAdminAPIRuntime` — sequential `buildAdminAPI{ACL,Auth,YouTubeStack,...}` calls with identical unwrap + cleanup. Functional options or builder chain.
4. `internal/app/runtime/lifecycle.go:52–73`, `http_server.go:31–76` — HTTP server start/shutdown mirrors patterns elsewhere; shared lifecycle abstraction candidate.
5. Settings interface/struct declarations: `SettingsActivityLogger`, `SettingsReadRecentLogsFunc`, `ConfigPublisher` (settings_handler.go:20–50) + APIHandler struct (api.go:45–78) — boilerplate.
6. CORS/origin validation: `internal/app/http/middleware.go:35–63` `normalizedOrigins`, `containsWildcard` overlap with `hololive-shared/pkg/server/middleware`.
