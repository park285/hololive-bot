# Contract Map

## Scope

현재 내부 HTTP JSON, Valkey queue, Valkey Pub/Sub, Iris external boundary 계약을 한눈에 추적합니다. RPC/gRPC 전환은 이 문서 범위가 아닙니다.

## Contract Inventory

| Contract | Provider | Consumer | Transport | Path/Event/Queue | Contract package | Version | Tests | Detail |
|---|---|---|---|---|---|---|---|---|
| `membernews` | `llm-scheduler` | `bot` | HTTP JSON | `/internal/membernews/subscriptions`, `/internal/membernews/digest` | `hololive/hololive-shared/pkg/contracts/membernews` | route constants, unversioned HTTP body | provider/client route tests | `contracts/membernews.md` |
| `majorevent` | `llm-scheduler` | `bot` | HTTP JSON | `/internal/majorevent/subscriptions` | `hololive/hololive-shared/pkg/contracts/majorevent` | route constants, unversioned HTTP body | provider/client route tests | `contracts/majorevent.md` |
| `trigger` | `llm-scheduler` | `admin-api` | HTTP JSON | `/internal/trigger/majorevent-weekly`, `/internal/trigger/majorevent-monthly`, `/internal/trigger/membernews-weekly` | `hololive/hololive-shared/pkg/contracts/trigger` | route constants, unversioned body | route/client tests | `contracts/trigger.md` |
| `alarm-http` | `admin-api` current registration; provider ownership 검토 필요 | `bot`, `admin-api` facade | HTTP JSON | `/internal/alarm/*` | shared handler/client under `hololive/hololive-shared/pkg/service/alarm` | unversioned | shared alarm API/client tests | `contracts/alarm.md` |
| `alarm-queue` | `alarm-worker` | `dispatcher-go` | Valkey list + sorted set + DLQ | `alarm:dispatch:queue`, `alarm:dispatch:retry`, `alarm:dispatch:dlq` | `hololive/hololive-shared/pkg/contracts/alarm` | `QueueEnvelopeVersionV1 = 1`, consumer also accepts `0` | queue contract/tests | `contracts/alarm.md` |
| `settings-pubsub` | admin/settings update path 검토 필요 | `bot`, `alarm-worker`, `llm-scheduler`, ingestion runtimes where subscriber configured | Valkey Pub/Sub | `config:update` | `hololive/hololive-shared/pkg/contracts/settings` | `ConfigUpdateVersionV1 = 1`; message has no `version` field | settings/configsub tests | `contracts/settings.md` |
| `iris-boundary` | Iris / Redroid | `bot`, `dispatcher-go` | External HTTP/H3 boundary | webhook/reply/send paths 검토 필요 | external boundary, no in-repo contract package | external | router/transport tests 검토 필요 | `contracts/iris-boundary.md` |

## Contract Change Rule

- Contract package, route constant, request/response shape, queue key, event type, error code가 바뀌면 이 문서와 개별 contract 문서를 함께 갱신합니다.
- Queue/PubSub 변경은 `QUEUE_AND_PUBSUB_CONTRACTS.md`도 갱신합니다.
- Error response 변경은 `ERROR_CONTRACT.md`도 갱신합니다.
- Provider/consumer가 불명확하면 `검토 필요`로 표시하고 확정처럼 쓰지 않습니다.

## Validation

```bash
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-error-contracts.sh
```
