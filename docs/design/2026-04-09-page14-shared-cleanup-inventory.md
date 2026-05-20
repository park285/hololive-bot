# Page 14 Shared Cleanup Inventory

Task 2 inventory for `docs/superpowers/plans/2026-04-09-page14-shared-cleanup-slice.md`.
This note captures the duplicate/provider pressure that remains after the `applyScraperProxyToggle` helper extraction and identifies the next smallest safe follow-up slice.

## Baseline after `applyScraperProxyToggle`

The shared helper now lives at:

- `hololive/hololive-shared/pkg/server/settings/scraper_proxy_toggle.go`

The remaining runtime-local wrappers are intentionally thin:

- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_proxy_toggle.go`
- `hololive/hololive-youtube-producer/internal/app/runtime_helpers.go`

This means the page-14 code slice already removed the duplicated proxy-toggle body, but did not yet reduce the surrounding runtime/provider duplication.

## Remaining duplicated `Close` wrappers

These files still carry the same nil-guard plus `CleanupCloser.Close()` wrapper shape over `shared-go/pkg/runtime/lifecycle/cleanup.go`:

- `hololive/hololive-kakao-bot-go/internal/app/runtime.go`
- `hololive/hololive-kakao-bot-go/internal/app/container.go`
- `hololive/hololive-kakao-bot-go/internal/app/db_integration_runtime.go`
- `hololive/hololive-kakao-bot-go/internal/app/fetch_profiles_runtime.go`
- `hololive/hololive-youtube-producer/internal/app/youtube_producer_runtime_runner.go`
- `hololive/hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go`
- `shared-go/pkg/runtime/lifecycle/cleanup.go`

Inventory note:

- This is real duplication, but it is thin-wrapper duplication only.
- The wrapper value is mostly API ergonomics for each runtime type.
- It is lower priority than provider duplication because removing it changes type-owned shutdown seams without reducing any runtime assembly branching.

## Duplicated `ProvideInfraResources`

These two provider functions are byte-for-byte duplicates:

- `hololive/hololive-kakao-bot-go/internal/app/providers/infra_resources.go`
- `hololive/hololive-youtube-producer/internal/app/providers/infra_resources.go`

Their returned struct is also duplicated:

- `hololive/hololive-kakao-bot-go/internal/app/providers/types.go`
- `hololive/hololive-youtube-producer/internal/app/providers/types.go`

Both runtimes immediately unwrap the same resource bag into runtime-local infra structs:

- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_core.go`
- `hololive/hololive-youtube-producer/internal/app/bootstrap.go`

Shared primitive ownership already exists underneath this seam:

- `hololive/hololive-shared/pkg/providers/infra_providers.go`

Inventory note:

- This is the cleanest remaining duplicate because both the provider body and the `InfraResources` shape are duplicated.
- The function already delegates entirely to shared providers for Valkey, Postgres, member repository, and member cache setup.
- The runtime-specific call sites only need the same data bag back; they do not need different orchestration logic here.

## Duplicated `ProvideYouTubeStack`

These two provider functions are also byte-for-byte duplicates:

- `hololive/hololive-kakao-bot-go/internal/app/providers/youtube.go`
- `hololive/hololive-youtube-producer/internal/app/providers/youtube.go`

The stack type itself is already shared through a type alias rather than duplicated concrete fields:

- `hololive/hololive-kakao-bot-go/internal/app/providers/types.go`
- `hololive/hololive-youtube-producer/internal/app/providers/types.go`
- `hololive/hololive-shared/pkg/providers/youtube_providers.go`

Current runtime-owned call sites:

- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_alarm_stack.go`
- `hololive/hololive-youtube-producer/internal/app/youtube_producer_runtime_builder.go`

Inventory note:

- The provider body is duplicated, but the surrounding assembly pressure is higher than `ProvideInfraResources`.
- The bot runtime passes alarm dispatch and formatter dependencies; the youtube-producer runtime passes `nil` for those runtime-specific seams.
- Moving this too early risks smuggling runtime orchestration decisions into `hololive-shared` instead of keeping the app packages responsible for their own wiring.

## Runtime orchestration pressure

The remaining pressure is not just duplicate functions. It is that runtime assembly is still split across local `app` packages while reusing lower-level shared providers:

- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_alarm_stack.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_core.go`
- `hololive/hololive-youtube-producer/internal/app/youtube_producer_runtime_builder.go`
- `hololive/hololive-youtube-producer/internal/app/bootstrap.go`
- `hololive/hololive-shared/pkg/providers/infra_providers.go`

Observed boundary:

- `hololive-shared/pkg/providers` currently owns primitive construction.
- The runtime `app` packages still own composition order, optional dependency selection, cleanup sequencing, and which runtime gets alarm-related behavior.
- That boundary is still healthy. The pressure shows up where local provider packages are just duplicating shared assembly without adding runtime-specific policy.

## Recommended next safe slice

Recommended next slice: deduplicate `ProvideInfraResources` and the matching `InfraResources` bag before touching `ProvideYouTubeStack` or the thin `Close` wrappers.

Why this is the safest next move:

- It is exact duplication in both function body and returned struct shape.
- It already sits directly on top of `hololive/hololive-shared/pkg/providers/infra_providers.go`.
- It does not require changing alarm/no-alarm runtime policy.
- Both current call sites in `bootstrap_core.go` and `bootstrap.go` can stay structurally identical after the extraction.

Why not start with the other leftovers:

- `ProvideYouTubeStack` duplication is real, but it sits closer to runtime-specific orchestration and optional dependency policy.
- `Close` wrapper dedupe has lower payoff and would spread API-surface changes across several runtime types for little architectural gain.
