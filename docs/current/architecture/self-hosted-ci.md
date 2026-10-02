# Self-hosted CI Boundary

Hololive Bot GitHub Actions workflows must not run untrusted pull request code on
repository self-hosted runners. Public or outside-contributor `pull_request`
workflows use GitHub-hosted `ubuntu-latest` runners so PR-controlled scripts,
tests, and package lifecycle hooks execute in GitHub-provisioned isolation.

The historical repository self-hosted Linux x64 runner label is `hololive-ci`.
Treat that runner as trusted-code only. Do not attach it to `pull_request`,
`pull_request_target` jobs that check out PR head code, or any workflow that
executes attacker-modifiable repository scripts.

## Trusted runner prerequisites

- Linux x64 host with outbound HTTPS access to GitHub.
- `bash`, `git`, `python3`, `unzip`, `strings`, `sudo`, `rg`, and Docker.
- Permission for the runner user to access Docker for workflow service
  containers.
- Go 1.27, or permission for `actions/setup-go` to install it.
- Rust toolchains, or permission for `dtolnay/rust-toolchain` to install them.
- Node.js 22, or permission for `actions/setup-node` to install it.

## Workflow split

- `*.yml` workflows that include `pull_request` run on `ubuntu-latest`.
- `ci.yml` is the secret-free PR fast gate.
- `security.yml` runs only on trusted non-PR events (`push` to `main`,
  `schedule`, and `workflow_dispatch`) and may use private-module credentials.
- Heavy full verification stays in the local pre-push gate
  (`scripts/ci/pre-push-gate.sh` -> `scripts/ci/local-ci.sh`).

## Blocking gates

The local pre-push gate provides the repository-level closeout gate and runs in
one pass:

- Content gates: release policy, the `scripts/**` shell-syntax sweep,
  deploy/runtime script tests (run only when the test or its target changed in
  the pushed range; always in `PRE_PUSH_MODE=full`), and
  `scripts/ci/local-ci.sh` for architecture contracts, Go toolchain pinning,
  `go mod tidy` drift, `go vet`, `staticcheck`, golangci-lint (formatters and
  modernize included), NilAway, build, unit and race tests, and PostgreSQL
  integration.
- Dependency hygiene: `go list -m -u` and `govulncheck` for the root module and
  every `go.work` module.
- Workspace compatibility: canonical module vet and the configured Go build/test scopes in `scripts/ci/local-ci.sh`.

A docs-only push (`docs/**` and `*.md` only, no `.go`/`.sh`/`.sql` under
`docs/`, exact push range, not `full`) skips the Go gates.

Self-hosted runner minutes are not charged as GitHub-hosted Actions minutes, but
that cost saving is not a security control. Use self-hosted capacity only for
trusted branch or manual operations where the checked-out code is already
trusted.
