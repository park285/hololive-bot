# Stream Ingester Structure Cleanup Design

**Date:** 2026-04-12

## Goal

Deep-clean `hololive/hololive-stream-ingester` so runtime assembly, YouTube ingestion control-plane logic, and operator/reporting tools have explicit ownership boundaries, smaller packages, and a reviewable migration path.

The cleanup must also preserve and first logically commit the current in-flight `stream-ingester` changes before larger package moves begin.

## Scope

- `hololive/hololive-stream-ingester/cmd`
- `hololive/hololive-stream-ingester/internal`
- local build wiring in `hololive/hololive-stream-ingester/Makefile` and Dockerfiles when path changes require it
- targeted references to moved runtime entrypoints in repo docs or compose files if any path-sensitive command changes are needed
- logical commit splitting for the existing dirty `stream-ingester` work before structural cleanup continues

## Non-goals

- changing ingestion behavior unless a structure move requires a narrow seam fix
- broad repo-wide refactoring outside `hololive-stream-ingester`
- production deploy or compose restart work
- rewriting unrelated historical report logic semantics during the structure pass

## Current Problems

### 1. Mixed command ownership

`cmd/` currently mixes long-running runtime binaries with one-shot operational/reporting utilities. That makes runtime entrypoints harder to scan and hides which binaries are part of normal service operation.

### 2. Flat `internal/app` ownership

`internal/app` currently acts as a catch-all package for:

- runtime bootstrap and lifecycle
- runtime infrastructure assembly
- YouTube poll target control logic
- cache warm helpers
- large reporting and dataset collectors

These concerns change for different reasons and should not share one package boundary.

### 3. Oversized reporting files

Several reporting/data collection files are large enough that they no longer represent one clearly legible unit. The worst current example is `community_shorts_alarm_sent_history_dataset.go`.

### 4. Dirty worktree collision risk

There are already uncommitted `stream-ingester` changes. A deep structure cleanup without first stabilizing those changes would blur behavior changes and file moves into one hard-to-review diff.

## Chosen Approach

Use a responsibility-first reorganization with explicit package seams under `internal/` and a clearer `cmd/` layout.

This is intentionally deeper than a file shuffle. The objective is to make package ownership visible from the path layout alone.

## Target Structure

### `cmd`

Keep user-buildable Go binaries under `cmd`, but split them into ownership groups:

- `cmd/runtime/stream-ingester`
- `cmd/runtime/youtube-scraper`
- `cmd/ops/...` for reporting, dataset, and audit utilities

If Go toolchain or Docker build ergonomics make nested `cmd/.../...` too awkward, keep the top-level `cmd/<binary>` paths stable and instead move shared `main` wiring into grouped internal packages. The preferred end state is grouped command paths, but behavior and build clarity take priority over cosmetic nesting.

### `internal/runtime`

Own long-running runtime assembly and lifecycle:

- bootstrap and infra assembly
- runtime feature specs
- HTTP server assembly
- readiness and lifecycle startup
- config subscriber wiring
- cache warm startup hook

This package should answer: “how does the runtime start and keep running?”

### `internal/youtube`

Own YouTube ingestion control-plane and scheduler-facing helpers:

- poll target resolution
- poll target refresh loop
- published-at resolver wiring
- poller registration construction
- route/policy helpers that exist to drive scraper scheduling

This package should answer: “how does YouTube polling decide what to watch and how to sync targets?”

### `internal/ops`

Own one-shot reporting, dataset collection, and operator analysis flows:

- community/shorts reports
- alarm sent history reports
- dataset builders
- observation query state helpers used only by ops/reporting flows

This package should answer: “how do we inspect or export ingestion-related evidence?”

### File decomposition inside `internal/ops`

Large report files should be split by concern, not arbitrarily by line count. For example, the alarm sent history dataset flow should separate:

- public collection entrypoint and request normalization
- row/result model types
- database load/query helpers
- comparison/finalization logic
- summary/stat aggregation helpers

## Commit Strategy

Before structural cleanup, stabilize the current dirty `stream-ingester` work into logical commits.

### Commit A

`feat(stream-ingester): refresh explicit youtube poll targets at runtime`

Contents:

- explicit notification/stats poll target resolution
- poll target refresher loop
- published-at resolver runtime wiring
- explicit poller registration target groups
- runtime lifecycle wiring required for those components
- direct tests for target resolution and refresh behavior

### Commit B

`refactor(stream-ingester): rebuild subscriber cache semantics and tighten test coverage`

Contents:

- cache rebuild helper rename/semantics alignment
- warm-start helper extraction
- related logger/test adjustments
- supporting concurrency-safe test helper cleanup

If review shows the current dirty changes are inseparable in practice, collapsing them into one commit is acceptable, but only after verifying that the combined commit still expresses one coherent behavior change.

## Migration Order

1. Inspect the current dirty `stream-ingester` diff and confirm the final logical split.
2. Run targeted tests for the dirty changes and commit them before file moves.
3. Introduce new internal package directories with minimal adapter moves first.
4. Move runtime assembly code into `internal/runtime`.
5. Move YouTube scheduler/control-plane helpers into `internal/youtube`.
6. Move reporting/data collection flows into `internal/ops`.
7. Decompose the largest `internal/ops` files until each file has one obvious reason to change.
8. Rewire command entrypoints and build artifacts only after internal imports stabilize.
9. Run targeted verification after each major seam move, then broader module verification at the end.

## Design Constraints

- Do not overwrite or revert unrelated dirty files in the repo.
- Keep public binary names stable: `stream-ingester` and `youtube-scraper`.
- Preserve existing runtime behavior and config semantics unless a targeted seam fix is required for correctness.
- Prefer move-then-trim over rewrite-from-scratch so diffs remain reviewable.
- Keep package names short and ownership-driven; avoid generic names like `common` or another catch-all `app`.

## Risks and Controls

### Risk: import churn hides behavioral regressions

**Control**

- separate logical commits from structure moves
- keep behavior-preserving moves isolated where possible
- run targeted tests after each seam move

### Risk: command path changes break build or Docker wiring

**Control**

- keep binary names unchanged
- update Dockerfiles and Makefile in the same slice as any `cmd` path move
- verify direct module builds before finalizing

### Risk: oversized ops files remain oversized after package moves

**Control**

- treat file decomposition as part of the cleanup definition of done
- do not stop after package renames if the largest files are still multi-purpose

## Verification Strategy

Minimum required verification across the work:

- targeted `go test` for touched `hololive-stream-ingester/internal/...` packages after each major move
- `go test ./hololive/hololive-stream-ingester/...`
- `go build ./hololive/hololive-stream-ingester/...`
- diff review to confirm runtime, youtube, and ops ownership is materially clearer than before

If command paths move:

- verify both runtime binaries still build from their new entrypoints
- verify Dockerfiles still reference the correct build targets

## Definition of Done

The cleanup is done only when all of the following are true:

- current dirty `stream-ingester` changes are committed in a logical way first
- runtime, YouTube control-plane, and ops/reporting code no longer share one catch-all `internal/app` package
- command ownership is clearer than the current mixed runtime-plus-ops layout
- the largest reporting files have been decomposed into narrower units
- targeted module build/tests pass for the touched area
- the final diff is reviewable as a sequence of coherent structural steps rather than one blended rewrite

## Execution Handoff

After user review of this design:

- write an implementation plan
- execute the logical commit split for the current dirty `stream-ingester` work
- perform the structural cleanup in the migration order above
