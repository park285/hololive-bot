# LLM Work Rules

## Scope

LLM이 `hololive-bot` 문서/계약 작업을 수행할 때 지켜야 하는 current 기준입니다.

## Minimum Context

작업 시작 시 최소한 다음을 제공합니다.

- `docs/current/PROJECT_MAP.md`
- 관련 task 또는 issue
- 변경할 runtime의 `docs/current/services/*.md`
- 관련 contract 문서와 `docs/current/CONTRACT_MAP.md`
- 관련 runbook

## Rules

- `docs/current`에는 현재 운영 기준만 둡니다.
- Historical handoff, closeout, 완료 기록은 `docs/history`에 둡니다.
- 확인되지 않은 provider, route, payload, error code, env는 확정하지 말고 `검토 필요`로 표시합니다.
- RPC/gRPC 전환을 문서 작업 중에 끼워 넣지 않습니다.
- 서비스 간 `internal` package import를 정당화하는 문서를 만들지 않습니다.
- Contract 변경은 code package, `CONTRACT_MAP.md`, 개별 contract 문서, error/queue 문서를 함께 검토합니다.
- Runtime 변경은 `PROJECT_MAP.md`, `SERVICE_OWNERSHIP.md`, service doc, runbook을 함께 검토합니다.
- 범위 밖 코드 리팩토링은 하지 않습니다.

## Result Report Format

```text
작업 ID:
변경 파일:
핵심 변경:
검증 명령:
검증 결과:
남은 리스크:
다음 추천 task:
```

## Required Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-current-docs-no-historical-body.sh
./scripts/architecture/check-current-docs-no-historical.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-error-contracts.sh
```

## Completion Rule

Passing a gate is not enough by itself. Before claiming completion, map every explicit task requirement to a concrete file, command, or inspection result.
