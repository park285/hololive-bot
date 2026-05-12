# TASK-D3-06. 계약 문서 추가: alarm

## Phase

D3. 내부 계약 문서

## 목표

`docs/current/contracts/alarm.md`를 작성하여 alarm queue 및 alarm HTTP 계약을 문서화합니다.

## 왜 필요한가

계약 문서는 provider와 consumer가 같은 이해를 갖도록 하는 SSOT입니다. 코드 contracts package에 있는 사실을 기준으로 작성합니다.

## 먼저 읽을 파일

- `hololive/hololive-shared/pkg/contracts/alarm/contracts.go`
- `hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`
- `hololive/hololive-shared/pkg/service/alarm/client.go`
- `hololive/hololive-alarm-worker/internal/app/build_runtime.go`
- `docs/current/CONTRACT_MAP.md`
- `templates/TEMPLATE_CONTRACT_DOC.md`

## 수정 또는 생성할 파일

- `docs/current/contracts/alarm.md`
- `docs/current/CONTRACT_MAP.md`

## 작업 단계

1. 계약 문서 템플릿을 사용합니다.
2. Provider, Consumer, Transport, Path/Event/Queue, Request, Response, Error codes, Timeout, Compatibility, Tests를 채웁니다.
3. 코드에서 확인된 사실만 확정 항목으로 씁니다.
4. 불확실하거나 provider가 모호한 항목은 '검토 필요'로 명시합니다.
5. CONTRACT_MAP.md의 해당 row와 링크를 맞춥니다.

## 금지 사항

- 계약 shape를 새로 발명하지 마십시오.
- 코드 DTO를 수정하지 마십시오.
- RPC/gRPC 설명을 추가하지 마십시오.

## 완료 조건

- `docs/current/contracts/alarm.md`가 생성됩니다.
- Contract Map에서 링크됩니다.
- 코드 contracts package와 모순되지 않습니다.
- 검토 필요 항목이 숨겨지지 않습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D3-06만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
