# Hololive

- Read ../AGENTS.md in iris-stack and affected subtree guidance. Run from root; [PROJECT_MAP](docs/current/PROJECT_MAP.md) owns topology. No standalone dashboard; web/credentials/native deployment belong to Iris web.
- Compile/test/build only on kapu; host compose.env owns bind addresses. hololive-bot-ops owns verified no-build remote cutovers; compose-redeploy-service.sh is a build-host deployment, not validation.
- Select ./build-all.sh --build-only --no-bump, affected Go/shared module tests or youtubejs npm test; docs need diff review. Publication requires scripts/ci/pre-push-gate.sh; preserve [README](README.md) verification ownership and pre-push-gate-phases-v1.
- PR/main: secret-free staged fast-gate; security.yml: non-PR main/schedule/manual. Keep heavy checks local; local-ci.sh excludes self-tests/dependency hygiene.
- Korean public contracts/side effects and reason comments; masked slog, fmt.Errorf("action: context: %w", err), context.Context first.
- Fix causes before suppressing findings; existing debt needs scoped fix or command/finding report. Bypasses require named false positive or approved emergency, smallest scope/reason/linter, nolintlint and retained regression check. Never hide directories/classes/test files; existing generated/third-party exclusions may remain.
- Stage 3/prerequisites and applicable NilAway/race stay blocking. Gate changes read stage configuration and phase contract; never weaken them.
- Dashboard: [boundary](admin-dashboard/AGENTS.md); migrations: [conventions](hololive/hololive-api/scripts/migrations/CONVENTIONS.md).
