# Hololive repository guidance

These rules apply across agent runtimes. In an iris-stack checkout, also read `../AGENTS.md`. Run commands from this repository root; read deeper `AGENTS.md` and `CONVENTIONS.md` for the affected subtree. `docs/current/PROJECT_MAP.md` owns the current module/runtime map.

## Ownership

The Go monorepo contains unified `hololive-api` (bot/admin/llm planes), alarm worker, `hololive/hololive-youtube-collector` (binary `youtube-collector`), shared libraries, and admin dashboard. The collector owns Holodex/Official Schedule/YouTube.js fetch, normalization, lease/checkpoint, and source-observation publishing. Its AP fleet is Osaka `youtube-collector-a`, Seoul `youtube-collector-b`, central `youtube-collector` (`c`), and Osaka2 `youtube-collector-d`.

Central runs on `hololive-osaka` (aarch64). Builds, images, and tests stay on `kapu`, also the CLIProxy/observability host; host `compose.env` owns bind addresses, not Compose defaults.

## Verification and deployment

Choose checks matching the change; docs-only edits normally need diff inspection. Required publish gates still apply.

```bash
./build-all.sh --no-bump
go build ./ ../shared-go/... ../iris-client-go/... ./admin-dashboard/backend/... ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/...
go test ./ ../shared-go/... ../iris-client-go/... ./admin-dashboard/backend/... ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/...
(cd hololive/hololive-youtube-collector/youtubejs && npm test)
```

Use `hololive-bot-ops` for local builds and remote no-build deployments under the global approval-scope rule. `./scripts/deploy/compose-redeploy-service.sh <service>` builds before cutover and must run on a build host; it is a deployment command, not validation.

## CI boundaries

PRs and main pushes run the secret-free staged gate (`policy`, `go-modules`, `frontend`, aggregated by `fast-gate`). `security.yml` remains non-PR (main push, schedule, manual dispatch).

`scripts/ci/pre-push-gate.sh` → `scripts/ci/local-ci.sh` owns required full tests, race detection, NilAway, PGO-off production policy, `check-workflow-secrets.sh`, the `scripts/**` shell-syntax sweep, and push-time govulncheck. Preserve `pre-push-gate-phases-v1`: commit-determined checks/conditional checker self-tests in `reusable`, `go list -m -u` and govulncheck in `freshness`, sibling `go.work` check in `ambient`. `local-ci.sh` runs neither self-tests nor dependency hygiene. Do not move blocking local checks into PR fast-gate or add a PR path to `security.yml`.

## Code and lint rules

- Document new/changed public APIs in Korean with contracts and side effects; internal comments explain reasons/invariants. Use `slog` with sensitive data masked, `fmt.Errorf("action: context: %w", err)`, and `context.Context` first in service/repository flows.
- Fix ownership, nil invariants, context, lifetime, wrapping, synchronization, and API-boundary causes before suppressing golangci-lint, NilAway, vet, staticcheck, gosec, or race findings. For exposed existing debt, make the smallest relevant fix or report the failing command and representative finding.
- `//nolint`, config exclusions, `RUN_NILAWAY=false`, `RUN_RACE_TESTS=false`, `--skip-local-ci`, and similar bypasses require a narrow named false positive or explicitly approved emergency. Scope to the smallest file/linter/command and give a concrete reason. Suppress only safe code the tool cannot model, name the linter, keep `nolintlint`, and retain a check that catches a real regression.
- Stage 3 remains blocking with all prerequisites: baseline `errcheck/govet/ineffassign/staticcheck/unused`; Stage 1 `bodyclose/noctx/errorlint/durationcheck/copyloopvar`; Stage 2 `gosec/exhaustive/contextcheck/unconvert/unparam/gocritic`; Stage 3 `errchkjson/makezero/prealloc/nestif/gocognit/nolintlint`.
- Keep NilAway and race blocking in local CI, pre-push, and security validation where applicable. Test exclusions need a documented named false positive. Never hide entire directories, linter classes, or test files to pass/promote a gate; existing generated-code and third-party exclusions may remain.

Dashboard rules are in `admin-dashboard/AGENTS.md`; migration rules include `hololive/hololive-api/scripts/migrations/CONVENTIONS.md`.
