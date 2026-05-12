# CI Gates

## Scope

Architecture and document gates keep current docs, contracts, runbooks, and governance assets aligned with code.

## Gate Order

`scripts/architecture/ci-boundary-gate.sh` currently runs:

1. M0 shared-go, removed runtime, artifact, import graph, and project map gates
2. M1 contract gates
3. M2 document contract gates
4. M4 LOC gates
5. M6 deprecated deadline and release governance gates

## Document Gates

| Gate | Script | Purpose | Failure condition | Exception policy |
|---|---|---|---|---|
| current-docs-no-historical | `check-current-docs-no-historical.sh` | Keep `docs/current` free of historical body markers | historical marker appears under `docs/current` | Use short bridge files without historical body markers |
| runbook-coverage | `check-runbook-coverage.sh` | Ensure all 7 runtime rows have runbook links and files | missing runtime/runbook/index link | Add runbook before linking from Project Map |
| contract-map | `check-contract-map.sh` | Ensure contract map, docs, and code package paths align | missing required contract doc/package/token | Mark uncertain provider as `검토 필요`, not omitted |
| internal-route-hardcoding | `check-internal-route-hardcoding.sh` | Keep internal routes centralized in contract/helper packages | hardcoded route appears outside allowed files | Add route constants before new call sites |
| repository-ownership | `check-repository-ownership.sh` | Keep data ownership and runtime internal imports aligned | forbidden runtime internal import or missing ownership token | Update ownership doc before adding shared repository access |
| error-contracts | `check-error-contracts.sh` | Ensure error docs cover stable contract codes and helpers | required error doc/helper token missing | Document compatibility gap before code changes |

## Local Validation

```bash
./scripts/architecture/check-current-docs-no-historical.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/check-error-contracts.sh
./scripts/architecture/ci-boundary-gate.sh
```
