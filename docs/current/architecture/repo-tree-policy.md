# Repository Tree Policy (`repo-tree-policy.md`)

## Scope

This document defines where repository-level files, docs, generated artifacts, local runtime data, and Go modules belong.

## Root

Root is reserved for repository entrypoints, workspace wiring, and repository-level config. Nothing else is tracked at root.

Allowed at root:

- Entrypoints and readme: `README.md`, `AGENTS.md`, `LICENSE`
- Release versioning: `VERSION` (release SSOT read by `scripts/check-release-version.sh`), `CHANGELOG.md`
- Go workspace: `go.work` and `go.work.sum`. The root holds no Go module; every module lives under `hololive/`
- Repository config: `.gitignore`, `.golangci.yml`, `.go-version`, `.python-version`, `.tool-versions`, `.dockerignore`
- Build entry script: `build-all.sh`
- Canonical env sample: `.env.example` (region- and host-specific `.env.*.example` live beside their compose files under `deploy/compose/`)
- Reserved local-runtime placeholders: `logs/`, `runtime-config/`, `artifacts/` (see §Local Runtime Data and §Artifacts)
- Module and content directories: `.github/`, `hololive/`, `deploy/`, `docs/`, `scripts/`

Not at root — these were relocated and must not return:

- Module tests belong beside their source; a root `go.mod`/`go.sum`/`doc.go` workspace anchor
- Local logs, backups, data dumps, generated review bundles, key/pem files, or unclassified design kits (covered by §Local Runtime Data and the artifact gate)

## Docs

- `docs/current/` contains current operational source-of-truth documents.
- `docs/current/` root is limited to core current docs and short compatibility bridges. Runbooks, service docs, contracts, architecture guidance, and review bundle policy belong in their purpose-specific subdirectories.
- `docs/history/` contains completed or non-current records.
- `docs/design/` contains proposals before they become current.
- Large old plan kits belong under `docs/history/` after classification.

## Artifacts

- Generated artifacts remain ignored unless a current governance document explicitly requires tracking them.

## Local Runtime Data

`logs/`, `data/`, `backups/`, `.review-bundles/`, key files, local env files, and generated archives are local machine state. They must not be deleted, moved, or tracked as part of tree cleanup unless a separate secret/artifact handling task explicitly approves it.

## Go Modules

Go module directories stay stable:

- `hololive/hololive-shared/`
- `hololive/hololive-api/`
- `hololive/hololive-alarm-worker/`
- `hololive/hololive-youtube-collector/`
- `hololive/hololive-dbtest/`

`go.work` also uses the sibling repositories `../shared-go` and `../iris-client-go`.

Package refactors must preserve `go.work`, Docker Compose build targets, runtime binary names, and architecture import boundary gates.

## Go Package Tree Depth

- Root packages that are part of an existing import contract should remain small facades or entrypoint wiring when their implementation grows beyond a single responsibility.
- Implementation files belong under role-specific internal packages such as `delivery`, `model`, `settings`, `httpserver`, `workerapp`, `collectorruntime`, or `alarmstore`.
- Further nested packages should be created by behavior family only when the new package has a stable contract and package-local tests.

## Validation

```bash
./scripts/architecture/check-tracked-local-artifacts.sh
```
