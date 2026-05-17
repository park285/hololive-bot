# Self-hosted CI

Hololive Bot GitHub Actions jobs run on a repository self-hosted Linux x64
runner with the custom label `hololive-ci`.

The runner must be registered in GitHub repository settings under
`Settings` > `Actions` > `Runners`. Register it with the `hololive-ci` custom
label and install it as a service.

## Runner prerequisites

- Linux x64 host with outbound HTTPS access to GitHub.
- `bash`, `git`, `python3`, `unzip`, `strings`, `sudo`, `rg`, and Docker.
- Permission for the runner user to access Docker for workflow service
  containers.
- Go 1.26.3, or permission for `actions/setup-go` to install it.
- Rust toolchains, or permission for `dtolnay/rust-toolchain` to install them.
- Node.js 22, or permission for `actions/setup-node` to install it.

## Blocking gates

The `Verify` workflow provides the repository-level closeout gate:

- `actionlint` for workflow syntax and custom runner label validation.
- `scripts/ci/local-ci.sh` for architecture contracts, admin-dashboard
  guardrails, Go toolchain pinning, `gofmt`, `go fix` drift, `go mod tidy`
  drift, `go vet`, `staticcheck`, build, unit tests, PostgreSQL integration,
  and `govulncheck`.
- A final `verify` job fails the workflow when any required gate fails or is
  cancelled.

Self-hosted runner minutes are not charged as GitHub-hosted Actions minutes,
but artifact and cache storage still count toward GitHub Actions storage usage.
