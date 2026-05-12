# TASK-D3-01. Contract Map 추가

## Phase

D3. 내부 계약 문서

## 목표

`docs/current/CONTRACT_MAP.md`를 추가하여 내부 HTTP API, Queue, Pub/Sub, Iris boundary 계약을 한눈에 정리합니다.

## 왜 필요한가

코드에는 contracts package가 있으나 문서상 전체 계약 지도가 없습니다. Contract Map이 없으면 provider/consumer 영향도를 LLM이 잘못 판단합니다.

## 먼저 읽을 파일

- `docs/current/PROJECT_MAP.md`
- `hololive/hololive-shared/pkg/contracts/majorevent/routes.go`
- `hololive/hololive-shared/pkg/contracts/membernews/routes.go`
- `hololive/hololive-shared/pkg/contracts/trigger/routes.go`
- `hololive/hololive-shared/pkg/contracts/settings/contracts.go`
- `hololive/hololive-shared/pkg/contracts/alarm/contracts.go`

## 수정 또는 생성할 파일

- `docs/current/CONTRACT_MAP.md`
- `docs/current/README.md`

## 작업 단계

1. 계약 목록 표를 작성합니다.
2. 각 계약에 Provider, Consumer, Transport, Path/Event/Queue, Contract package, Version, Tests, Runbook link를 적습니다.
3. 확정되지 않은 alarm HTTP API provider는 '검토 필요'로 표시합니다.
4. Iris boundary는 internal contract가 아니라 external boundary로 분리합니다.
5. current README에 Contract Map을 등록합니다.

## 금지 사항

- 코드 contract를 수정하지 마십시오.
- 아직 없는 API를 확정된 계약으로 쓰지 마십시오.
- RPC/gRPC 항목을 추가하지 마십시오.

## 완료 조건

- CONTRACT_MAP.md가 생성됩니다.
- membernews, majorevent, trigger, settings, alarm queue, Iris boundary가 포함됩니다.
- 각 계약에 provider와 consumer가 있습니다.
- 검토 필요 항목이 숨겨지지 않습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D3-01만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
