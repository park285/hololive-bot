# 04. 검증 매트릭스

## 공통 검증

| 변경 종류 | 필수 검증 |
|---|---|
| Project Map 변경 | `./scripts/architecture/check-project-map.sh` |
| go.work 변경 | `go work sync`, `check-project-map.sh` |
| runtime 문서 변경 | `check-runbook-coverage.sh` |
| contract 문서 변경 | `check-contract-map.sh` |
| internal route 문서/코드 변경 | `check-internal-route-hardcoding.sh` |
| error code 문서/코드 변경 | `check-error-contracts.sh` |
| release governance 변경 | `check-release-governance-assets.sh` |
| 전체 architecture 영향 | `./scripts/architecture/ci-boundary-gate.sh` |

## 수동 검증

문서 PR이라도 다음을 확인해야 합니다.

- 루트 README가 현재 runtime 기준과 다르지 않은지
- `docs/current/README.md`가 current 문서만 가리키는지
- current 문서에 `CLOSED / HISTORICAL` 같은 상태가 남아 있지 않은지
- Project Map의 runtime 수가 `go.work` 및 compose와 맞는지
- runtime별 runbook 링크가 존재하는지
- Contract Map의 contract package path가 실제 존재하는지
- PR template이 계약 변경을 묻는지

## 실패 시 대응

문서 gate가 실패하면 문서를 고치는 것이 원칙입니다. gate를 완화하려면 `docs/current/architecture/ci-gates.md`에 이유와 만료 시점을 적어야 합니다.
