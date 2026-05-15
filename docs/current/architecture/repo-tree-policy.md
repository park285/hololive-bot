# Repository Tree Policy (`repo-tree-policy.md`)

## Scope

This document defines where repository-level files, docs, generated artifacts, local runtime data, and Go modules belong.

## Root

Root is reserved for repository entrypoints and workspace-level contracts:

- `README.md`
- `AGENTS.md`
- `go.work`, root `go.mod`, and `go.work.sum`
- Docker Compose files
- build, test, deploy, and architecture gate entry scripts
- repository-level config such as `.gitignore`, `.golangci.yml`, `.go-version`, `.tool-versions`

Root must not accumulate local logs, backups, data dumps, generated review bundles, or unclassified design kits.

## Docs

- `docs/current/` contains current operational source-of-truth documents.
- `docs/history/` contains completed or non-current records.
- `docs/design/` and `docs/superpowers/specs/` contain proposals before they become current.
- `docs/superpowers/plans/` contains executable implementation plans.
- Large old plan kits under `docs/holobot-*` or similar names must be classified before being moved.

## Artifacts

- `artifacts/architecture/go-workspace-import-graph.txt` is the tracked architecture artifact.
- Other generated artifacts should remain ignored unless a current governance document explicitly requires tracking them.

## Local Runtime Data

`logs/`, `data/`, `backups/`, `.review-bundles/`, key files, local env files, and generated archives are local machine state. They must not be deleted, moved, or tracked as part of tree cleanup unless a separate secret/artifact handling task explicitly approves it.

## Go Modules

Go module directories stay stable:

- `shared-go/`
- `hololive/hololive-shared/`
- `hololive/hololive-admin-api/`
- `hololive/hololive-alarm-worker/`
- `hololive/hololive-dispatcher-go/`
- `hololive/hololive-kakao-bot-go/`
- `hololive/hololive-llm-sched/`
- `hololive/hololive-stream-ingester/`

Package refactors must preserve `go.work`, Docker Compose build targets, runtime binary names, and architecture import boundary gates.

## Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-current-docs-no-historical-body.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-tracked-local-artifacts.sh
```
