## shared-go

- Files: go=24, test=18, ratio=75%
- Local guides: `shared-go/AGENTS.md` (no CONVENTIONS.md)

### LOC thresholds (Top 5)

1. `pkg/logging/logging_archive.go`: 320 / UNLISTED-LARGE — archives compressed logs; 12 helpers.
2. `pkg/logging/logging.go`: 215 (thresholds.txt lists 520 — actual is 215, threshold-file stale).
3. `pkg/telemetry/telemetry.go`: 156 — `MapCarrier`, Provider init.
4. `pkg/jsonutil/extract.go`: 141 — JSON extraction with fence+bracket fallback.
5. `pkg/envutil/env.go`: 137.

Threshold-file drift: `shared-go/pkg/logging/logging.go:520` is outdated (file is 215 lines). Worth correcting in Phase 2.

### Function budget (Top 5)

1. `pkg/telemetry/telemetry.go:56` `NewProvider` ~56 lines (otel setup + sampler config).
2. `pkg/logging/logging_archive.go:216` `appendArchivedCompressedBackup` ~27 lines.
3. `pkg/httputil/response.go:91` `newAPIError` ~18 lines.
4. `pkg/logging/logging_archive.go:137` `pruneArchivedCompressedBackups` ~16 lines.
5. `pkg/jsonutil/extract.go:59` `extractFirstJSON` ~16 lines.

All under 60-line ceiling; `NewProvider` is the only near-budget case.

### Test coverage gaps

1. `pkg/logging/operation.go` (94 LOC) — 7 funcs (RunOperation, operationContext*, eventOrDefault) untested.
2. `pkg/logging/log.go` (56 LOC) — context attrs wrappers (Debug/Info/Warn/Error) lack isolated tests.
3. `pkg/logging/id.go` (56 LOC) — ID generation, prefix sanitization untested.
4. `pkg/jsonutil/extract.go` — only one ~45-line test; malformed JSON edge cases not covered.
5. `pkg/httputil/client.go` — `applyTransportProfile`, `baseProfiledTransport`, timeout/profile composition untested.

By-package ratios: envutil/ginjson/stringutil/telemetry/workerpool/httputil/json near 100%; logging/jsonutil ~50% (missing operation.go, id.go).

### Naming inconsistencies

1. `new*` factory pattern followed by `newConsoleHandler`, `newCompressedLogArchiver`, `newAPIError`, but helpers like `sanitizeIDPrefix`, `errorType`, `withString` mix prefixes.
2. Context helpers: `stringFromContext` vs `withString` — `With*`/`*FromContext` convention not uniform.
3. `compressedLogArchiver` methods `archiveCompressedLogFiles`, `matchingCompressedBackupNames`, `pruneArchivedCompressedBackups` are standalone helpers rather than receiver methods.
4. `jsonBracketMatcher` is unexported but constructed via `newJSONBracketMatcher` — exported constructor naming for unexported type is inconsistent.
5. HTTP errors: `newAPIError` (unexported) vs `CheckStatus` (exported) — surface inconsistency.

### Duplication / extraction candidates

1. HTTP transport profile/timeout helpers in `httputil/client.go` + `httputil/json_client.go` overlap with `hololive-shared/pkg/server/internal/httpserver/runtime_helpers.go`.
2. Context attribute helpers: `logging/context.go` (`WithJobID`, `WithRuntime`, …) + `logging/operation.go` duplicate context-carrying logic — unify into `ContextBuilder`.
3. Telemetry propagation: `telemetry/MapCarrier` + `logging/OTelHandler` both touch span extraction/injection.
4. Error wrapping: `httputil/response.go` `APIError` + `logging/attrs.go` `ErrorAttrs` both convert errors to attrs.
5. JSON fence/bracket extraction in `jsonutil/extract.go` is shared-go-only; likely duplicated ad-hoc in app code.
6. ID generation: `logging/id.go` `NewID` + `operation.go` context setup could expose `NewOperationID(prefix)` factory.
