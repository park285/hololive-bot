# CI Gates

## Scope

Architecture gates keep code boundaries, SQL/migration ownership, deploy configuration and
portable document links aligned. They do not check document coverage or wording; the stack
AGENTS.md forbids doc-sync, self-test and removed-code guard gates.

## Gate Order

`scripts/architecture/ci-boundary-gate.sh` runs, in order: shared-go and package-name
boundaries, tracked local artifacts, alarm contract values and route hardcoding, migration
manifest, SQL ownership, DB access policy, markdown local paths, runtime import boundaries,
notification egress ownership, topology parity, deploy/compose/log script tests, and deprecated removal deadlines. Non-test Go files are capped at 800 lines by golangci-lint
`revive` `file-length-limit` and functions at 120 lines by `funlen`. Code complexity is checked by
the configured linters; separate duplicate structure-budget gates are not required.

The final-image scan never suppresses findings: `scripts/ci/final-image-scan-policy.sh` owns the Trivy
arguments, and `scripts/ci/check-recurring-security-scan-contract.sh` checks the security configuration.
The actual final-image scan validates the built images. Scanner self-tests, workflow/script text markers,
retired-name grep guards, and historical fixture/owner hash locks are not product validation gates.

## Document Gates

| Gate | Script | Purpose | Failure condition | Exception policy |
|---|---|---|---|---|
| generic-go-internal-package-names | `check-go-generic-internal-package-names.sh` | Keep moved Go implementations under role-specific package names instead of generic buckets | `internal/core`, `servicecore`, `package core`, `package servicecore`, or `import core "..."` appears under active Go modules | Rename the package to the behavior family it owns |
| doc-links-no-local-paths | `check-doc-links-no-local-paths.sh` | Keep markdown links portable on GitHub and clones | local machine path marker appears in markdown docs | Use repository-relative links |
| internal-route-hardcoding | `check-internal-route-hardcoding.sh` | Keep internal routes centralized in contract/helper packages | hardcoded route appears outside allowed files | Add route constants before new call sites |
| repository-ownership | `check-repository-ownership.sh` | Keep data ownership and runtime internal imports aligned | forbidden runtime internal import | Update ownership doc before adding shared repository access |

## Local Validation

```bash
./scripts/architecture/check-go-generic-internal-package-names.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/ci-boundary-gate.sh
```
