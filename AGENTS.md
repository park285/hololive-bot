# AGENTS.md

Cross-runtime project canon for the `hololive-bot` monorepo.
Keep this file limited to always-needed project rules across runtimes.

## Project Identity

This repository is a Go monorepo with central and AP-host deployments.
It includes the unified `hololive-api` runtime (bot/admin/llm planes in one process), the alarm worker, the YouTube collector module (`hololive/hololive-youtube-collector`, binary `youtube-collector`; fetch uses collector-owned Holodex, Official Schedule, and YouTube.js helpers), shared libraries, and the admin dashboard. The collector runs as a four-member AP fleet: Osaka `youtube-collector-a`, Seoul `youtube-collector-b`, central `youtube-collector` (`c`), and Osaka2 `youtube-collector-d`. External fetch, normalization, lease/checkpoint, and source-observation publishing remain collector-owned.

The central runtime host is `hololive-osaka` (`aarch64`); builds, images, and tests stay on the `kapu` workstation, which also hosts CLIProxy and the observability stack. Central bind addresses are owned by each host's `compose.env`, not by Compose defaults.

## Working Defaults

1. Run commands from the repository root.
2. Use deeper subtree `AGENTS.md` files before changing code in specialized modules.
3. Use subtree `CONVENTIONS.md` files when they exist.
4. Use `docs/current/PROJECT_MAP.md` for the current module map and ownership boundaries.

## Verification Commands

```bash
./build-all.sh --no-bump
go build ./ ../shared-go/... ../iris-client-go/... ./admin-dashboard/backend/... ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/...
go test ./ ../shared-go/... ../iris-client-go/... ./admin-dashboard/backend/... ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/...
(cd hololive/hololive-youtube-collector/youtubejs && npm test)
./scripts/deploy/compose-redeploy-service.sh <service>
```

## Repo Rules

- Avoid code comments unless the code would be hard to understand without one; write necessary comments in Korean.
- Keep CI weight placement fixed — do not regress it: the public repository runs a secret-free staged gate (`policy`, `go-modules`, `frontend`, aggregated by `fast-gate`) on pull requests and pushes to `main`, while `security.yml` remains non-PR (`push` to `main`, schedule, and manual dispatch). ALL verification still runs in the local pre-push gate (`scripts/ci/pre-push-gate.sh` → `scripts/ci/local-ci.sh`), including full tests, race detection, NilAway, the PGO-off production policy, workflow-boundary validation (`check-workflow-secrets.sh`), the `scripts/**` shell-syntax sweep, and push-time govulncheck. The gate follows the iris-stack `pre-push-gate-phases-v1` contract: commit-determined checks and conditionally run checker self-tests in `reusable`, `go list -m -u` plus govulncheck in `freshness`, the `go.work` sibling check in `ambient`; `local-ci.sh` itself runs no self-tests and no dependency hygiene. Do not move those blocking local checks into the PR fast gate or add a PR path to `security.yml`.
- Use `slog` for Go logging and mask sensitive data before logging.
- Use `fmt.Errorf(\"action: context: %w\", err)` for wrapped errors.
- Pass `context.Context` as the first argument in Go service and repository flows.

## Quality Gate Discipline

- Treat `golangci-lint`, NilAway, `go vet`, `staticcheck`, `gosec`, and race detector findings as design feedback first. Prefer fixing the underlying ownership, nil invariant, context propagation, resource lifetime, error wrapping, synchronization, or API boundary issue before adding a suppression.
- Use `//nolint`, config exclusions, `RUN_NILAWAY=false`, `RUN_RACE_TESTS=false`, `--skip-local-ci`, or similar bypasses only for a narrow, named false positive or an explicitly approved emergency path. Keep the bypass scoped to the smallest file, linter, and command surface, and include a concrete reason.
- When a stricter gate exposes existing debt, either make the smallest root-cause fix in the touched area or record the residual debt as a follow-up with the failing command and representative finding. Do not broaden blanket excludes to make a gate green.
- If a suppression is unavoidable, require a specific linter name and explanation (`nolintlint` must stay enabled), then add or keep a verification command that would fail if the real bug reappears.

## Lint Ratchet Stages

- Stage 3 is the current blocking `golangci-lint` gate. Keep the baseline (`errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`), Stage 1 (`bodyclose`, `noctx`, `errorlint`, `durationcheck`, `copyloopvar`), Stage 2 (`gosec`, `exhaustive`, `contextcheck`, `unconvert`, `unparam`, `gocritic`), and Stage 3 (`errchkjson`, `makezero`, `prealloc`, `nestif`, `gocognit`, `nolintlint`) enabled together.
- Keep NilAway and race detector blocking in local CI, pre-push, and security validation where applicable. Do not exclude test files from NilAway or lint unless a named false positive is documented.
- Clear new findings through root-cause code fixes first. Accept suppressions only when the code is already safe, the tool cannot model the invariant, and the line carries a concrete reason.
- Do not promote or preserve a gate by hiding whole directories, broad linter classes, test files, or generated blanket exclusions. Generated code and third-party dependency directories may remain excluded.

## Reference

Use `docs/current/PROJECT_MAP.md` for structure and ownership.
Use subtree guides such as `admin-dashboard/AGENTS.md` and subtree `CONVENTIONS.md` files (e.g. `hololive/hololive-api/scripts/migrations/CONVENTIONS.md`) for local module rules.
