# TASK-D6-09. DLQ replay runbook 추가

## Phase

D6. Runbook

## 목표

`docs/current/runbooks/dlq-replay.md`를 작성하여 alarm dispatch DLQ 확인/재처리 절차를 문서화합니다.

## 왜 필요한가

Alarm queue consumer는 invalid payload를 DLQ로 보존합니다. DLQ가 쌓였을 때 운영자가 어떻게 확인하고 재처리할지 문서가 필요합니다.

## 먼저 읽을 파일

- `hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`
- `hololive/hololive-shared/pkg/contracts/alarm/contracts.go`
- `docs/current/QUEUE_AND_PUBSUB_CONTRACTS.md`

## 수정 또는 생성할 파일

- `docs/current/runbooks/dlq-replay.md`
- `docs/current/runbooks/README.md`

## 작업 단계

1. DLQ key와 retry queue key를 명시합니다.
2. DLQ 확인 명령을 작성합니다.
3. raw payload 보존 정책을 설명합니다.
4. 재처리 전 검토 체크리스트를 작성합니다.
5. 재처리 명령은 안전하지 않으면 pseudo-command로 두고 실제 명령은 검토 필요로 표시합니다.
6. unsupported version과 invalid JSON 처리 차이를 설명합니다.

## 금지 사항

- DLQ replay 자동화 스크립트를 이 task에서 만들지 마십시오.
- 실제 운영 명령을 확신 없이 작성하지 마십시오.

## 완료 조건

- DLQ runbook이 생성됩니다.
- QUEUE_AND_PUBSUB_CONTRACTS와 링크됩니다.
- 재처리 전 안전 체크리스트가 있습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D6-09만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
