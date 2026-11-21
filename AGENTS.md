# Hololive

- Work from the repository root. Use the inherited iris-stack guidance. For service boundaries use [PROJECT_MAP](docs/current/PROJECT_MAP.md); the management UI belongs to `iris-console`.
- For deployment use [current runbooks](docs/current/runbooks/README.md) and `hololive-bot-ops`, not historical handoffs. Compile/test/build only on kapu; remote cutovers use verified artifacts without rebuilding. Host `compose.env` owns bind addresses.
- For development, run the affected Go/shared module or helper tests. Review docs directly. For image builds use `./build-all.sh --build-only --no-bump`; for authorized publication use `scripts/ci/pre-push-gate.sh`. See [verification ownership](README.md#개발-및-검증-development--test).
- GitHub Release create/edit: follow the [release runbook](docs/current/runbooks/release.md) through `scripts/publish-release.sh` only (preview first, GitHub-generated notes only).
- PR/main use the secret-free `fast-gate`. `security.yml` owns non-PR main/scheduled/manual scans. Local build CI excludes dependency hygiene; pre-push runs `govulncheck`.
- Language work must apply the corresponding installed modern-language skill (Go: `use-modern-go`; JavaScript/TypeScript: `modern-javascript-typescript`) alongside task skills.
- No comments or doc comments (stack ban; machine directives only, enforced by `bash scripts/ci/verify-tools.sh scan`). Intent goes into names, types, tests and docs; change history, decision IDs and references go in the commit message. Masked slog, fmt.Errorf("action: context: %w", err), context.Context first.
- Fix causes before suppressing findings; existing debt needs scoped fix or command/finding report. Bypasses require named false positive or approved emergency, smallest scope/reason/linter, nolintlint and retained regression check. Never hide directories/classes/test files; existing generated/third-party exclusions may remain.
- For gate changes, use the current `.golangci.yml`, CI entry points and [gate inventory](docs/current/architecture/ci-gates.md). Keep Stage 3 lint and its prerequisites, applicable NilAway/race, and product checks blocking; a retired name alone is not a defect.
- Migrations: [conventions](hololive/hololive-api/scripts/migrations/CONVENTIONS.md).
