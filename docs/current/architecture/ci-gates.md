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
`revive` `file-length-limit` and functions at 120 lines by `funlen`. Non-test `.rs`/`.sh`/`.ts`/`.tsx`
files (800 lines), Go function complexity 16 and nesting 8, and the `_partN` file ban are enforced by
the iris-stack meta pre-push hook through `tools/structure/check_repo_budgets.py`. That check runs only
when the meta repository is pushed, not on a hololive-bot push, so a violation merged here fails the next
meta push; run `python3 tools/structure/check_repo_budgets.py --stack-root . --policy tools/structure/repo-budgets.json`
from the meta checkout before publishing a structural change.

The final-image scan never suppresses findings: `scripts/ci/final-image-scan-policy.sh` owns the Trivy
arguments, and `scripts/ci/check-recurring-security-scan-contract.sh` runs the real scanner against a stub
`trivy` to prove the invocation (security.yml, and pre-push when its inputs change).

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
