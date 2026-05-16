# CI Gates

## Scope

Architecture and document gates keep current docs, contracts, runbooks, and governance assets aligned with code.

## Gate Order

`scripts/architecture/ci-boundary-gate.sh` currently runs:

1. M0 shared-go, removed runtime, artifact, import graph, and project map gates
2. M1 contract gates
3. M2 document contract gates
4. M4 LOC and function budget gates
5. M6 deprecated deadline and release governance gates

## Document Gates

| Gate | Script | Purpose | Failure condition | Exception policy |
|---|---|---|---|---|
| generic-go-internal-package-names | `check-go-generic-internal-package-names.sh` | Keep moved Go implementations under role-specific package names instead of generic buckets | `internal/core`, `servicecore`, `package core`, `package servicecore`, or `import core "..."` appears under active Go modules | Rename the package to the behavior family it owns |
| current-docs-no-historical-body | `check-current-docs-no-historical-body.sh` | Keep `docs/current` free of historical body markers while allowing short bridge files | historical body marker appears under `docs/current` | Use short bridge files without historical body markers |
| current-docs-root-allowlist | `check-current-docs-root-allowlist.sh` | Keep `docs/current` root limited to core SSOT files and approved compatibility bridges | unclassified root-level file appears under `docs/current` | Move runbooks, services, contracts, architecture guidance, review policy, or history records to their purpose-specific subdirectory |
| doc-links-no-local-paths | `check-doc-links-no-local-paths.sh` | Keep markdown links portable on GitHub and clones | local machine path marker appears in markdown docs | Use repository-relative links |
| runbook-coverage | `check-runbook-coverage.sh` | Ensure all 7 runtime rows have runbook links, files, and required sections | missing runtime/runbook/index link or required section | Add runbook content before linking from Project Map |
| contract-map | `check-contract-map.sh` | Ensure contract map, manifest, docs, and code package paths align | missing required contract doc/package/token/manifest row | Mark uncertain provider as `검토 필요`, not omitted |
| internal-route-hardcoding | `check-internal-route-hardcoding.sh` | Keep internal routes centralized in contract/helper packages | hardcoded route appears outside allowed files | Add route constants before new call sites |
| repository-ownership | `check-repository-ownership.sh` | Keep data ownership and runtime internal imports aligned | forbidden runtime internal import or missing ownership token | Update ownership doc before adding shared repository access |
| error-contracts | `check-error-contracts.sh` | Ensure error docs cover stable contract codes and helpers | required error doc/helper token missing | Document compatibility gap before code changes |
| function-budget | `check-function-budget.sh` | Keep Go production functions within Iris-level defaults: 60 lines, complexity 8, nesting 5 | any production Go function exceeds lines, complexity, or nesting defaults | Refactor until every function passes the default budget; baseline exceptions are not allowed |

## Local Validation

```bash
./scripts/architecture/check-current-docs-no-historical-body.sh
./scripts/architecture/check-current-docs-no-historical.sh
./scripts/architecture/check-current-docs-root-allowlist.sh
./scripts/architecture/check-go-generic-internal-package-names.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/check-error-contracts.sh
./scripts/architecture/check-function-budget.sh
./scripts/architecture/ci-boundary-gate.sh
```
