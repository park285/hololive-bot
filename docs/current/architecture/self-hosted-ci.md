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
- Go 1.26, or permission for `actions/setup-go` to install it.
- Rust toolchains, or permission for `dtolnay/rust-toolchain` to install them.
- Node.js 22, or permission for `actions/setup-node` to install it.

## Workflow split

- `*.yml` workflows that include `pull_request` run on `ubuntu-latest`.
- `*-trusted.yml` workflows run equivalent trusted `push` and
  `workflow_dispatch` gates on `[self-hosted, linux, x64, hololive-ci]`.
- Do not add `pull_request` triggers to `*-trusted.yml` workflows.

## Blocking gates

The `Verify` and `Verify Trusted` workflows provide the repository-level
closeout gate:

- `actionlint` for workflow syntax and custom runner label validation.
- `scripts/ci/local-ci.sh` for architecture contracts, admin-dashboard
  guardrails, Go toolchain pinning, `gofmt`, `go fix` drift, `go mod tidy`
  drift, `go vet`, `staticcheck`, build, unit tests, PostgreSQL integration,
  and `govulncheck`.
- A final `verify` job fails the workflow when any required gate fails or is
  cancelled.

Self-hosted runner minutes are not charged as GitHub-hosted Actions minutes, but
that cost saving is not a security control. Use self-hosted capacity only for
trusted branch or manual operations where the checked-out code is already
trusted.
