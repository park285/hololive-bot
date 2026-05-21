## admin-dashboard

- Files (backend): rs=47, tests=2, ratio=4.3%
- Files (frontend): ts/tsx=131, tests=9, ratio=6.9%
- Local guides: `admin-dashboard/AGENTS.md`, `admin-dashboard/frontend/README.md`

### LOC thresholds

Backend (Top 5):
1. `backend/src/handlers/auth.rs`: 1308 / 1320 (99%) — NEAR (effectively at ceiling).
2. `backend/src/auth/session.rs`: 701 / 720 (97%) — NEAR.
3. `backend/src/config.rs`: 389 / 400 (97%) — NEAR.
4. `backend/src/auth/middleware.rs`: 386 / UNLISTED-LARGE.
5. `backend/src/error.rs`: 347 / UNLISTED-LARGE.

Frontend (Top 3):
1. `frontend/src/components/settings/DockerContainerList.tsx`: 329 / 140 (235%) — OVER (no listed ceiling for this component; pattern-set 140 applied).
2. `frontend/src/api/generated/Admin.ts`: 635 (generated; out of scope).
3. `frontend/src/api/generated/data-contracts.ts`: 454 (generated; out of scope).

### Function/Method/Component budget

Backend (Top 5):
1. `backend/src/handlers/auth.rs:124` `handle_login` ~97 lines.
2. `backend/src/handlers/auth.rs:247` `handle_session_status` ~122 lines.
3. `backend/src/handlers/auth.rs:369` `handle_heartbeat` ~71 lines.
4. `backend/src/holo/handlers_queries.rs:32–312` — 12 async proxy handlers, mostly thin.
5. `backend/src/holo/handlers_commands.rs:32–312` — 13 async proxy handlers, mostly thin.

Frontend (Top 3):
1. `frontend/src/components/settings/DockerContainerList.tsx:28` `DockerContainerList` ~300-line JSX body with 3 useState / 2 useQuery / 4 useMutation.
2. `frontend/src/features/alarms/components/AlarmGroups.tsx:47` `AlarmGroups` ~200 lines.
3. `frontend/src/hooks/useMembersPage.ts` `useMembersPage` ~238 lines.

### Test coverage gaps

Backend:
1. `backend/src/auth/session.rs` — 701 lines / 14 test attributes; `Session::rotate()`, `SessionRefreshResult`, `is_absolutely_expired_at()` uncovered.
2. `backend/src/auth/middleware.rs` — 386 lines / 19 tests; `extract_cookie` edge cases (malformed, missing delimiter) uncovered.
3. `backend/src/config/security.rs` — 205 lines / 6 tests; CORS allow-list and CSRF mode transitions untested.
4. `backend/src/holo/handlers_{commands,queries}.rs` — 620 combined lines, no integration tests for upstream 5xx / malformed responses.

Frontend:
1. `DockerContainerList` action confirmation flow + toast error handling — untested.
2. `useHeartbeat` — single timing test; no timeout/recovery on network failure.
3. `useWebSocket` (229 lines) — reconnection backoff and malformed message handling untested.
4. `MembersPage`/`MilestonesPage`/`StreamsPage` — no `.test.tsx`; only selectors tested.

### Naming inconsistencies

1. `holo/handlers.rs` is a 73-byte barrel re-export; `handlers_commands.rs` / `handlers_queries.rs` hold impl — naming suggests `handlers.rs` should be primary.
2. `useMemberMutations()` exports 6 mutation hooks (`useAddAliasMutation`, …) — hook is a factory bundle vs `useHeartbeat()` (effect) vs `useMembersPage()` (orchestration); category mixing.
3. Rust State extractor naming inconsistent: `State(state)` (auth.rs) vs `State(app_state)` (status.rs).
4. `ValkeySessionStore` in code but docs and comments retain "Redis" — partial migration vocabulary.
5. `authApi.login()` (frontend, verb suffix) vs `handle_login()` (backend, verb prefix) — symmetry broken.

### Duplication / extraction candidates

1. Auth handler boilerplate (`handle_login`, `handle_session_status`, `handle_heartbeat`): extract State + session validation + `AppError` response — macro or shared extractor.
2. Cookie setters `set_session_cookie` / `set_csrf_cookie` / `set_clear_cookie` (middleware.rs:70–100) — unify into `CookieBuilder`.
3. `useQuery` + `useMutation` + `toast.error()` + `invalidateQueries` pattern repeats in `DockerContainerList`, `AlarmGroups`, every modal. Extract `useMutationWithToast()`.
4. Status handlers (`handle_aggregated_status`, `handle_system_stats_stream`, `handle_docker_health`) all call `state.status_collector.collect()` or docker svc → JSON. Extract status middleware or trait.
5. Feature pages (`Members`, `Streams`, `Milestones`, `Alarms`, `Rooms`) share toolbar + content + loader layout. Extract `<FeaturePage>` wrapper.
6. `AppError::into_response()` + `ErrorResponse::from_value()` boilerplate (error.rs) — Result type alias with `?` ergonomics.
