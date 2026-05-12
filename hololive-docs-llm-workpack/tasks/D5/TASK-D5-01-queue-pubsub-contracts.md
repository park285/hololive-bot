# TASK-D5-01. Queue/PubSub Contract 문서 추가

## Phase

D5. Queue/PubSub 계약

## 목표

`docs/current/QUEUE_AND_PUBSUB_CONTRACTS.md`를 작성하여 alarm dispatch queue와 config update Pub/Sub 계약을 문서화합니다.

## 왜 필요한가

Queue와 Pub/Sub은 HTTP API가 아니기 때문에 Contract Map만으로 부족합니다. version, retry, DLQ, invalid payload 처리, 유실 가능성을 별도 문서로 고정해야 합니다.

## 먼저 읽을 파일

- `hololive/hololive-shared/pkg/contracts/alarm/contracts.go`
- `hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`
- `hololive/hololive-shared/pkg/contracts/settings/contracts.go`
- `hololive/hololive-shared/pkg/service/configsub/subscriber.go`
- `hololive/hololive-shared/pkg/service/configsub/dispatcher.go`

## 수정 또는 생성할 파일

- `docs/current/QUEUE_AND_PUBSUB_CONTRACTS.md`
- `docs/current/README.md`
- `docs/current/contracts/alarm.md`
- `docs/current/contracts/settings.md`

## 작업 단계

1. Alarm dispatch queue key, retry queue key, DLQ key, version을 문서화합니다.
2. Consumer가 v0/v1을 읽고 unsupported version을 거부한다는 현재 동작을 기록합니다.
3. invalid JSON과 delayed retry wrapper 손상 payload가 DLQ로 보존되는 동작을 기록합니다.
4. Config Pub/Sub channel, current shape, update types를 문서화합니다.
5. Pub/Sub 유실 가능성과 startup refresh 필요성을 명시합니다.
6. 명령성 이벤트는 Pub/Sub보다 trigger API가 적합하다는 원칙을 적되, 현재 구조를 즉시 변경하지 않습니다.

## 금지 사항

- queue envelope v2 설계를 확정하지 마십시오.
- Pub/Sub 메시지 포맷을 코드에서 바꾸지 마십시오.

## 완료 조건

- QUEUE_AND_PUBSUB_CONTRACTS.md가 생성됩니다.
- Alarm queue와 settings Pub/Sub이 모두 포함됩니다.
- DLQ와 retry 정책이 설명됩니다.
- 계약별 문서와 링크됩니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D5-01만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
