## hololive-llm-sched

- Files: go=112, test=44, ratio=39%
- Local guides: none

### LOC thresholds (Top 5)

1. `internal/service/membernews/filter/filter.go`: 389 / UNLISTED-LARGE — 22 small helpers ~17 lines each.
2. `internal/service/majorevent/summarizer/summarizer_consensus.go`: 382 / UNLISTED-LARGE — consensus review logic.
3. `internal/app/internal/runtime/bootstrap_llm_scheduler.go`: 378 / UNLISTED-LARGE — bootstrap & component assembly.
4. `internal/service/membernews/repository.go`: 374 / UNLISTED-LARGE — DB operations.
5. `internal/service/membernews/source_validator.go`: 348 / UNLISTED-LARGE — URL validation + tier resolution.

Thresholded references:
- `internal/llm/openai_client.go`: 336 / 430 (78%) — Responses API + chat fallback + schema extraction.
- `internal/service/majorevent/scraper/link_checker.go`: 266 / 440 (60%).

### Function budget (Top 5)

1. `internal/app/internal/runtime/bootstrap_llm_scheduler.go:217` `buildLLMSchedulerComponents` ~55 lines, builder over ~15 helpers.
2. `internal/app/internal/runtime/bootstrap_llm_scheduler.go:68` `Run()` ~28 lines.
3. `internal/llm/openai_client.go:59` `NewClient` ~46 lines.
4. `internal/app/internal/runtime/bootstrap_llm_scheduler.go:274` `newLLMSchedulerRuntime` ~26 lines.
5. All `filter.go` functions are under 30 lines; nothing exceeds the 60-line ceiling.

### Test coverage gaps

1. `cmd/llm-scheduler` 0 tests — main entrypoint.
2. `internal/app` 0 tests — type alias module only.
3. `internal/model` 0 tests — search.go model types.
4. `internal/service/consensus` 0 tests — undocumented consensus contract.
5. `internal/llm` 5 prod / 1 test — only `openai_client_test.go`; `client.go`, `openai_response_diagnostics.go`, `openai_provider_errors.go`, `openai_fallback.go` untested.
6. `internal/service/majorevent` 4 prod / 1 test — repository and errors untested.

### Naming inconsistencies

1. Two `LLMClient` interfaces: `internal/llm/Client` vs `summarizer.LLMClient` (`internal/service/majorevent/summarizer/llm_client.go`).
2. `Scheduler` type name collision across `internal/service/membernews/scheduler/scheduler.go` and `internal/service/majorevent/scheduler/scheduler.go` — no disambiguating suffix.
3. Filename mirrors package name: `scheduler/scheduler.go`, `filter/filter.go`.
4. `llmSchedulerFormatter` (unexported) vs `LLMSchedulerRuntime` (exported) — inconsistent casing for "LLM".
5. Model trees: `internal/service/membernews/internal/model/` and `internal/model/search.go` overlap.

### Duplication / extraction candidates

1. Internal route registration: `api_internal_majorevent.go:37` and `api_internal_membernews.go` repeat APIKeyAuth + route group + handler wiring.
2. LLM client init: `llm_providers_local.go:32` (`ProvideMajorEventLLMClient`) and `ProvideMemberNewsLLMClient` repeat `cliproxy.Enabled` / APIKey / BaseURL / Model validation + option build.
3. Scheduler component build: `bootstrap_llm_scheduler.go:241` invokes `buildMajorEventComponents` and `buildMemberNewsComponents` with parallel Scheduler/MonthlyScheduler/formatter wiring.
4. Repository split pattern: majorevent has 3 repository files (`repository.go`, `repository_events.go`, `repository_schema.go`); membernews has 2; inconsistent split intent.
5. Error wrap pattern already normalized — but consensus/majorevent paths still repeat `fmt.Errorf("scope: %w", err)` shaped manually.
