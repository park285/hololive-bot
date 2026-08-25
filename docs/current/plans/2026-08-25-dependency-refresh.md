# Hololive Dependency Refresh Implementation Plan

**Goal:** Refresh all Hololive Bot Go and npm dependencies to the latest compatible stable releases and publish the validated source snapshot.

**Architecture:** Each Go module remains independently tidy with `GOWORK=off`; npm applications retain their current major-version contracts while package manifests and lockfiles move together. Toolchains, Docker base images, public API breaks, runtime deployment, and release tags are outside this change.

**Success criteria:** Every compatible registry update is applied or explicitly rejected with evidence, module and lockfile graphs are reproducible, focused package checks and the repository pre-push gate pass, and the final commit is available on `origin/main` through the required status-check workflow.

**Decisions:** None. This plan records a bounded maintenance operation and introduces no plan-surviving architecture choice.

**Execution:** Use `executing-plans` with `update_plan`. Delegation is not authorized.

---

## Context and constraints

- Current source includes the premiere notification, Holodex HTTP/2 fix, and coordinated `v3.3.0` release metadata.
- The user explicitly authorized broad dependency and lockfile updates plus remote Git writes.
- Preserve module paths, public contracts, fallback behavior, and the Go `1.27.0` toolchain. Do not cross npm or Go module major paths.
- Keep `github.com/kapu/* v0.0.0` workspace placeholders unchanged. Update published `iris-client-go/v2` and `shared-go/v2` only if a newer compatible release exists.
- Do not deploy dependency-updated artifacts or create/push `v3.3.0`; both are separately gated operations.
- Do not touch unrelated meta-repository changes.

## File map

- Modify: `go.work.sum`, `admin-dashboard/backend/go.{mod,sum}`, and `hololive/*/go.{mod,sum}` for compatible Go graph updates.
- Modify: `admin-dashboard/frontend/package.json` and `package-lock.json` for compatible frontend updates.
- Modify: `hololive/hololive-youtube-collector/youtubejs/package.json` and `package-lock.json` when compatible helper updates exist.
- Modify: `CHANGELOG.md` with the exact dependency refresh summary.

### Task 1: Refresh Go module graphs

- [x] Run `GOWORK=off go get -u ./...` and `GOWORK=off go mod tidy` in each owned Go module.
- [x] Review direct dependency changes and reject any major-path, unpublished pseudo-version, or toolchain drift.
- [x] Run `GOWORK=off go test ./...` in each changed module, then the repository workspace drift and production provenance checks.

### Task 2: Refresh npm graphs

- [x] Update direct dependencies to the latest version allowed by their existing major contract and regenerate both lockfiles with the pinned npm runtime.
- [x] Run frontend lint, typecheck, test, and build; run YouTube.js typecheck and its 117-test suite.
- [x] Reject any update that requires a public contract, Node engine, or framework-major migration.

### Task 3: Record, validate, and publish

- [x] Add exact top-level dependency changes to `CHANGELOG.md` and run `bash scripts/check-release-version.sh`.
- [x] Run `scripts/ci/local-ci.sh` through the guarded pre-push workflow; failures remain terminal until fixed or the offending bump is removed.
- [ ] Commit the dependency refresh, push a bounded remote branch to obtain the required `fast-gate`, then fast-forward `origin/main` only after the status succeeds.

## Failure, security, and stop rules

- Do not turn failed, incompatible, or ambiguous upgrades into success. Remove only the offending bump or make an evidence-backed source adaptation.
- Preserve secret-free package-manager output and do not add registries, credentials, install scripts, or runtime permissions.
- Stop before any major-version migration, new runtime dependency, release tag, production rebuild, or deployment that requires authorization beyond this source dependency refresh.

## Handoff state

The active source branch and this plan are the canonical execution inputs. Final evidence is the clean `hololive-bot` worktree, successful package checks, successful guarded push, and matching local/remote commit SHA.
