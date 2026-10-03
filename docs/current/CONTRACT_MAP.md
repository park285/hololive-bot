# Contract Map

## Scope

현재 내부 HTTP JSON, PostgreSQL outbox, Valkey Pub/Sub, Iris external boundary와 public redirect 계약을 한눈에 추적합니다. RPC/gRPC 전환은 이 문서 범위가 아닙니다.

## Contract Inventory

| Contract ID | Provider | Consumer | Transport | Path/Event/Queue | Contract package | Version | Tests | Detail |
|---|---|---|---|---|---|---|---|---|
| `x.spaces.session` | `hololive-api` admin plane | Iris Console, alarm-worker | HTTP JSON + PostgreSQL | `/api/holo/x-spaces/session`; `x_space_session` | `hololive/hololive-shared/pkg/service/xspaces` | revision fenced candidate/active | session DB tests, generated web contracts | `services/x-spaces.md` |
| `membernews.digest` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/membernews/digest` | `hololive/hololive-shared/pkg/contracts/membernews` | route constants, unversioned HTTP body | provider/client route tests | `contracts/membernews.md` |
| `membernews.subscription` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/membernews/subscriptions` | `hololive/hololive-shared/pkg/contracts/membernews` | route constants, unversioned HTTP body | provider/client route tests | `contracts/membernews.md` |
| `majorevent.subscription` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/majorevent/subscriptions` | `hololive/hololive-shared/pkg/contracts/majorevent` | route constants, unversioned HTTP body | provider/client route tests | `contracts/majorevent.md` |
| `trigger.manual` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/trigger/majorevent-weekly`, `/internal/trigger/majorevent-monthly`, `/internal/trigger/membernews-weekly` | `hololive/hololive-shared/pkg/contracts/trigger` | route constants, unversioned body | route/client tests | `contracts/trigger.md` |
| `alarm.http` | `alarm-worker` | `hololive-api` bot/admin clients | HTTP JSON | `/internal/alarm/*` | `hololive/hololive-shared/pkg/service/alarm` | unversioned | shared alarm API/client tests; actual worker HTTP roundtrip tests | `contracts/alarm.md` |
| `alarm.dispatch` | `alarm-worker` | `alarm-worker` | PostgreSQL dispatch outbox + payload-free Valkey wakeup | `alarm_dispatch_events` (room-agnostic payload), `alarm_dispatch_deliveries` (per-room state incl. `retry`/`dlq`/`quarantined`); wakeup list `alarm:dispatch:wakeup` | `hololive/hololive-shared/pkg/contracts/alarm` (envelope version); outbox in `hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox` | `QueueEnvelopeVersionV1 = 1`; publisher rejects any other version | envelope fixture tests, publisher/dispatchoutbox tests | `contracts/alarm.md` |
| `youtube.outbox.egress` | `hololive-api` | `alarm-worker` | PostgreSQL outbox table | `youtube_notification_outbox` rows; alarm-worker owns claim, render, per-room delivery, and final send state | `hololive/hololive-shared/pkg/service/youtube/outbox` | table schema | outbox dispatcher tests | `contracts/alarm.md` |
| `alarm.state.read` | `alarm-worker` (data owner) | `hololive-api` | PostgreSQL table read | `alarms` direct reads: YouTube plane `notification_channel_ids.sql` (projection tx, `members` JOIN, graduated excluded, `LIMIT`); llm plane membernews `repository_query_0080_03.sql`. Bot/admin planes use `alarm.http`; API config and both builders require `ALARM_INTERNAL_URL` | `hololive/hololive-api/internal/planes/youtube/runtime` (`notification_channel_ids.sql`); llm plane membernews SQL | SQL asset, unversioned | YouTube runtime `TestRuntimeSQLAssetsLoad`; `check-repository-ownership.sh` (youtube-collector import ban) | `contracts/alarm.md` |
| `shortlink.youtube` | `hololive-api` | browsers, KakaoTalk scraper, `alarm-worker` grouped message renderer | External HTTPS redirect | `GET`, `HEAD` `/l/:videoID` | `hololive/hololive-shared/pkg/contracts/shortlink` | route constants, unversioned | route, origin, scraper rejection, render tests | `contracts/shortlink.md` |
| `settings.update` | `hololive-api` admin-plane settings API | `iris-console` (admin caller), `alarm-worker` (`PUT /internal/alarm/settings` apply, `settings.json` startup restore) | HTTP JSON + shared settings file; no Pub/Sub | `POST /api/holo/settings`, `POST /api/holo/settings/llm`, `settings.json` | `hololive/hololive-shared/pkg/contracts/settings` (response key only) | unversioned | admin settings apply-once test | `contracts/settings.md` |
| `iris.webhook` | Iris / Redroid | `hololive-api`, `alarm-worker` | External HTTP/H3 boundary | webhook/reply/send paths 검토 필요 | external boundary, no in-repo contract package | external | router/transport tests 검토 필요 | `contracts/iris-boundary.md` |

## Source Observation Ownership

Source observation의 공용 envelope·canonical JSON·hash·lease 계약은 `hololive/hololive-shared/pkg/contracts/sourceobservation`이 소유합니다. Collector `internal/runtime/sourceobservation`은 publish/checkpoint, API `internal/youtube/sourceobservation`은 consume/canonical/replay/retention을 소유합니다. 실제 양 구현을 연결하는 교차 DB 시험은 module-root testkit을 사용하며, upcoming candidate의 canonical clock 시험은 worker `internal/service/alarm/dispatchoutbox`에 있습니다. [Canonical JSON v1](contracts/source-observation-canonical-json-v1.md).

## Contract Change Rule

- Contract package, route constant, request/response shape, queue key, event type, error code가 바뀌면 이 문서와 개별 contract 문서를 함께 갱신합니다.
- Queue/PubSub 변경은 `QUEUE_AND_PUBSUB_CONTRACTS.md`도 갱신합니다.
- Error response 변경은 `ERROR_CONTRACT.md`도 갱신합니다.
- Contract ID/provider/consumer/package/doc 변경은 `CONTRACT_MANIFEST.txt`도 갱신합니다.
- Provider/consumer가 불명확하면 `검토 필요`로 표시하고 확정처럼 쓰지 않습니다.

## Validation

```bash
./scripts/architecture/check-internal-route-hardcoding.sh
```
