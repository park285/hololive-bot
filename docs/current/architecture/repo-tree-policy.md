# Repository Tree Policy (`repo-tree-policy.md`)

## Scope

This document defines where repository-level files, docs, generated artifacts, local runtime data, and Go modules belong.

## Root

Root is reserved for repository entrypoints, workspace wiring, and repository-level config. Nothing else is tracked at root.

Allowed at root:

- Entrypoints and readme: `README.md`, `AGENTS.md`, `LICENSE`
- Go workspace: `go.work`, `go.work.sum`, root `go.mod`/`go.sum`, and `doc.go` (the root `package workspace` anchor)
- Repository config: `.gitignore`, `.golangci.yml`, `.go-version`, `.tool-versions`, `.dockerignore`
- Build entry script: `build-all.sh`
- Canonical env sample: `.env.example` (region- and host-specific `.env.*.example` live beside their compose files under `deploy/compose/`)
- Reserved local-runtime placeholders: `logs/`, `runtime-config/`, `artifacts/` (see §Local Runtime Data and §Artifacts)
- Module and content directories: `.github/`, `hololive/`, `admin-dashboard/`, `deploy/`, `docs/`, `scripts/`, `internal/`

Not at root — these were relocated and must not return:

- Renovate config → `.github/renovate.json`
- Workspace-level Go contract tests and their fixtures → `internal/workspace/` (with `internal/workspace/testdata/`); only `doc.go` stays at root as the package anchor
- Local logs, backups, data dumps, generated review bundles, key/pem files, or unclassified design kits (covered by §Local Runtime Data and the artifact gate)

## Docs

- `docs/current/` contains current operational source-of-truth documents.
- `docs/current/` root is limited to core current docs and short compatibility bridges. Runbooks, service docs, contracts, architecture guidance, and review bundle policy belong in their purpose-specific subdirectories.
- `docs/history/` contains completed or non-current records.
- `docs/design/` and `docs/superpowers/specs/` contain proposals before they become current.
- `docs/superpowers/plans/` contains executable implementation plans.
- Large old plan kits under `docs/holobot-*` or similar names belong under `docs/history/plan-kits/` after classification.

## Artifacts

- `artifacts/architecture/go-workspace-import-graph.txt` is the tracked architecture artifact.
- Other generated artifacts should remain ignored unless a current governance document explicitly requires tracking them.

## Local Runtime Data

`logs/`, `data/`, `backups/`, `.review-bundles/`, key files, local env files, and generated archives are local machine state. They must not be deleted, moved, or tracked as part of tree cleanup unless a separate secret/artifact handling task explicitly approves it.

## Go Modules

Go module directories stay stable:

- `shared-go/`
- `hololive/hololive-shared/`
- `hololive/hololive-api/`
- `hololive/hololive-alarm-worker/`
- `hololive/hololive-youtube-producer/`

Package refactors must preserve `go.work`, Docker Compose build targets, runtime binary names, and architecture import boundary gates.

## Go Package Tree Depth

- Root packages that are part of an existing import contract should remain small facades or entrypoint wiring when their implementation grows beyond a single responsibility.
- Implementation files belong under role-specific internal packages such as `delivery`, `polling`, `scraping`, `model`, `settings`, `httpserver`, `botruntime`, `workerapp`, or `reports`.
- Generic buckets are not allowed for new or moved Go code: do not use `internal/core`, `servicecore`, `package core`, or `import core "..."`.
- Further nested packages should be created by behavior family only when the new package has a stable contract and package-local tests.

## Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-current-docs-root-allowlist.sh
./scripts/architecture/check-go-generic-internal-package-names.sh
./scripts/architecture/check-current-docs-no-historical-body.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-tracked-local-artifacts.sh
```
