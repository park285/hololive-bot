# 03. 전역 작업 규칙

모든 task에 적용되는 규칙입니다.

## 절대 하지 말 것

- RPC/gRPC 관련 설계를 추가하지 마십시오.
- 문서 정리 task에서 대규모 코드 리팩토링을 하지 마십시오.
- historical 문서를 삭제하지 마십시오. 이동하거나 bridge를 남기십시오.
- runtime 이름을 임의로 바꾸지 마십시오.
- `docs/current/PROJECT_MAP.md`와 `go.work`, `docker-compose.prod.yml`을 불일치 상태로 두지 마십시오.
- 서비스 간 `internal` package import를 정당화하는 문서를 만들지 마십시오.
- 계약 문서를 작성하면서 코드 contracts package와 다른 path/error code를 invent하지 마십시오.

## 문서 작성 원칙

- current 문서는 현재 운영 기준만 담습니다.
- history 문서는 과거 의사결정과 완료 기록을 담습니다.
- design 문서는 아직 적용되지 않은 설계 제안을 담습니다.
- 문서에는 owner, scope, non-goals, related files, validation을 포함합니다.
- 문서가 코드와 불일치할 때는 코드가 맞다는 가정을 하지 말고 “검토 필요”로 표시합니다.
- 확정되지 않은 API provider는 확정된 것처럼 쓰지 않습니다.

## LLM 작업 결과 보고 형식

각 task 완료 후 LLM은 다음 형식으로 보고해야 합니다.

```text
작업 ID:
변경 파일:
핵심 변경:
검증 명령:
검증 결과:
남은 리스크:
다음 추천 task:
```

## 검증 우선순위

가능하면 다음을 실행합니다.

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/ci-boundary-gate.sh
go test . -run TestRuntimeSplitStandaloneModulesContract
```

문서 gate를 추가한 뒤에는 추가 gate도 실행합니다.

```bash
./scripts/architecture/check-current-docs-no-historical.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-error-contracts.sh
```
