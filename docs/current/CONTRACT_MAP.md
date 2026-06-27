# Contract Map

## Scope

현재 내부 HTTP JSON, Valkey queue, Valkey Pub/Sub, Iris external boundary 계약을 한눈에 추적합니다. RPC/gRPC 전환은 이 문서 범위가 아닙니다.

## Contract Inventory

| Contract ID | Provider | Consumer | Transport | Path/Event/Queue | Contract package | Version | Tests | Detail |
|---|---|---|---|---|---|---|---|---|
| `membernews.digest` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/membernews/digest` | `hololive/hololive-shared/pkg/contracts/membernews` | route constants, unversioned HTTP body | provider/client route tests | `contracts/membernews.md` |
| `membernews.subscription` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/membernews/subscriptions` | `hololive/hololive-shared/pkg/contracts/membernews` | route constants, unversioned HTTP body | provider/client route tests | `contracts/membernews.md` |
| `majorevent.subscription` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/majorevent/subscriptions` | `hololive/hololive-shared/pkg/contracts/majorevent` | route constants, unversioned HTTP body | provider/client route tests | `contracts/majorevent.md` |
| `trigger.manual` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/trigger/majorevent-weekly`, `/internal/trigger/majorevent-monthly`, `/internal/trigger/membernews-weekly` | `hololive/hololive-shared/pkg/contracts/trigger` | route constants, unversioned body | route/client tests | `contracts/trigger.md` |
| `alarm.http` | `alarm-worker`; `hololive-api` admin-plane compatibility | `hololive-api` (bot + admin-plane facade) | HTTP JSON | `/internal/alarm/*` | `hololive/hololive-shared/pkg/service/alarm` | unversioned | shared alarm API/client tests; shared alarm route registrar test | `contracts/alarm.md` |
| `alarm.dispatch` | `alarm-worker` | `alarm-worker` | Valkey list + sorted set + DLQ | `alarm:dispatch:queue`, `alarm:dispatch:retry`, `alarm:dispatch:dlq` | `hololive/hololive-shared/pkg/contracts/alarm` | `QueueEnvelopeVersionV1 = 1`, consumer also accepts `0` | queue contract/tests | `contracts/alarm.md` |
| `youtube.outbox.egress` | `youtube-producer` | `alarm-worker` | PostgreSQL outbox table | `youtube_notification_outbox` rows; alarm-worker owns claim, render, per-room delivery, and final send state | `hololive/hololive-shared/pkg/service/youtube/outbox` | table schema | outbox dispatcher tests | `contracts/alarm.md` |
| `karing.kakaolink` | `alarm-worker` | `iris` | External HTTP JSON + KakaoLink template handoff | `/karing/content-list`; KakaoLink list templates `133266`, `133223`, `133222`, `133267` | external | template ID and Kakao Developers variable contract | alarm-worker Karing request tests, Iris Karing smoke | `contracts/karing-kakaolink.md` |
| `settings.update` | `hololive-api` current publisher through admin-plane settings update paths | `hololive-api`, `alarm-worker`, `youtube-producer`, ingestion runtimes where subscriber configured | Valkey Pub/Sub | `config:update` | `hololive/hololive-shared/pkg/contracts/settings` | `ConfigUpdateVersionV1 = 1`; message has no `version` field | settings/configsub tests | `contracts/settings.md` |
| `iris.webhook` | Iris / Redroid | `hololive-api`, `alarm-worker` | External HTTP/H3 boundary | webhook/reply/send paths 검토 필요 | external boundary, no in-repo contract package | external | router/transport tests 검토 필요 | `contracts/iris-boundary.md` |

## Contract Change Rule

- Contract package, route constant, request/response shape, queue key, event type, error code가 바뀌면 이 문서와 개별 contract 문서를 함께 갱신합니다.
- Queue/PubSub 변경은 `QUEUE_AND_PUBSUB_CONTRACTS.md`도 갱신합니다.
- Error response 변경은 `ERROR_CONTRACT.md`도 갱신합니다.
- Contract ID/provider/consumer/package/doc 변경은 `CONTRACT_MANIFEST.txt`도 갱신합니다.
- Provider/consumer가 불명확하면 `검토 필요`로 표시하고 확정처럼 쓰지 않습니다.

## Validation

```bash
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-error-contracts.sh
```
