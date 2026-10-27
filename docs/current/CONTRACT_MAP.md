# Contract Map

## Scope

현재 내부 HTTP JSON, PostgreSQL outbox, Valkey Pub/Sub, Iris external boundary와 public redirect 계약을 한눈에 추적합니다. RPC/gRPC 전환은 이 문서 범위가 아닙니다.

## Contract Inventory

| Contract ID | Provider | Consumer | Transport | Path/Event/Queue | Contract package | Version | Tests | Detail |
|---|---|---|---|---|---|---|---|---|
| `x.spaces.session` | `hololive-api` admin plane | Iris Console, alarm-worker | HTTP JSON + PostgreSQL | `/api/holo/x-spaces/session`; `x_space_session` | `hololive/hololive-shared/pkg/service/xspaces` | revision fenced candidate/active | session DB tests, generated web contracts | `services/x-spaces.md` |
| `membernews.digest` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/membernews/digest` | `hololive/hololive-api/internal/contracts/membernews` | route constants, unversioned HTTP body | provider/client route tests | `contracts/membernews.md` |
| `membernews.subscription` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/membernews/subscriptions` | `hololive/hololive-api/internal/contracts/membernews` | route constants, unversioned HTTP body | provider/client route tests | `contracts/membernews.md` |
| `majorevent.subscription` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/majorevent/subscriptions` | `hololive/hololive-api/internal/contracts/majorevent` | route constants, unversioned HTTP body | provider/client route tests | `contracts/majorevent.md` |
| `trigger.manual` | `hololive-api` | `hololive-api` | HTTP JSON | `/internal/trigger/majorevent-weekly`, `/internal/trigger/majorevent-monthly`, `/internal/trigger/membernews-weekly` | `hololive/hololive-api/internal/contracts/trigger` | route constants; typed queued/skipped/partial enqueue result, not delivery proof | route/client/result tests | `contracts/trigger.md` |
| `alarm.http` | `alarm-worker` | `hololive-api` bot/admin clients | HTTP JSON | `/internal/alarm/*` | route constants: `hololive/hololive-shared/pkg/contracts/alarm`; client: `hololive/hololive-shared/pkg/service/alarmhttp`; server handler·registrar: `hololive/hololive-alarm-worker/internal/service/alarm/httpapi` | unversioned | shared client tests; worker handler·registrar tests and actual handler/client roundtrip tests | `contracts/alarm.md` |
| `alarm.dispatch` | `alarm-worker` | `alarm-worker` | PostgreSQL dispatch outbox + payload-free Valkey wakeup | `alarm_dispatch_events` (room-agnostic payload), `alarm_dispatch_deliveries` (per-room state incl. `retry`/`dlq`/`quarantined`); wakeup list `alarm:dispatch:wakeup` | `hololive/hololive-shared/pkg/contracts/alarm` (envelope version); outbox in `hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox` | `QueueEnvelopeVersionV1 = 1`; publisher rejects any other version | envelope fixture tests, publisher/dispatchoutbox tests | `contracts/alarm.md` |
| `youtube.outbox.egress` | `hololive-api` | `alarm-worker` | PostgreSQL outbox table | `youtube_notification_outbox` rows; alarm-worker owns claim, render, per-room delivery, and final send state | payload JSON: `hololive/hololive-shared/pkg/contracts/youtubeoutbox` (`Video`, `Short`, `Community`); shared SQL: `hololive/hololive-shared/pkg/service/youtube/outbox/deliverysql` | table schema; payload field names, order and omitempty fixed to stored rows | payload JSON shape tests, outbox dispatcher tests | `contracts/alarm.md` |
| `notification.delivery.payload` | `hololive-api` intent producer | `alarm-worker` | PostgreSQL JSONB | `notification_delivery_outbox.payload` | `hololive/hololive-shared/pkg/contracts/notificationdelivery` (`Payload`, `Request`) | existing field names/order/omission and request CAS bytes unchanged | stored-byte fixtures, producer enqueue and worker request/rearm tests | `PROJECT_MAP.md` |
| `alarm.state.read` | `alarm-worker` (data owner) | `hololive-api` | PostgreSQL table read | `alarms` direct reads: YouTube plane `notification_channel_ids.sql` (projection tx, `members` JOIN, graduated excluded, `LIMIT`); llm plane membernews `repository_query_0080_03.sql`. Bot/admin planes use `alarm.http`; API config and both builders require `ALARM_INTERNAL_URL` | `hololive/hololive-api/internal/planes/youtube/runtime` (`notification_channel_ids.sql`); llm plane membernews SQL | SQL asset, unversioned | YouTube runtime `TestRuntimeSQLAssetsLoad`; the alarms write repository stays in alarm-worker `internal/service/alarm/alarmstore` (Go `internal` visibility) | `contracts/alarm.md` |
| `shortlink.youtube` | `hololive-api` | browsers, KakaoTalk scraper, `alarm-worker` grouped message renderer | External HTTPS redirect | `GET`, `HEAD` `/l/:videoID` | `hololive/hololive-shared/pkg/contracts/shortlink` | route constants, unversioned | route, origin, scraper rejection, render tests | `contracts/shortlink.md` |
| `settings.update` | `hololive-api` admin-plane settings API | `iris-console` (admin caller), `alarm-worker` (`PUT /internal/alarm/settings` apply, `settings.json` startup restore) | HTTP JSON + shared settings file; no Pub/Sub | `POST /api/holo/settings`, `POST /api/holo/settings/llm`, `settings.json` | `hololive/hololive-api/internal/contracts/settings` (response key only) | unversioned | admin settings apply-once test | `contracts/settings.md` |
| `iris.webhook` | Iris / Redroid | `hololive-api`, `alarm-worker` | External HTTP/H3 boundary | webhook/reply/send paths 검토 필요 | external boundary, no in-repo contract package | external | router/transport tests 검토 필요 | `contracts/iris-boundary.md` |

관리자 멤버 쓰기는 기존 생성 201·변경 200과 `status`·`message`를 유지하며 저장·캐시 결과를 `persisted`·`cacheSynced`·`memberId`·`replayed`로 구분합니다. 동일 생성 입력만 기존 행의 동기화를 복구하고 불일치는 409입니다. [API 서비스 계약](services/hololive-api.md#관리자-멤버-쓰기)의 Console projection 제한을 따릅니다. `settings.update`의 두 요청 경계는 알 수 없는 필드를 400으로 거부하며 다른 API의 decoder 정책을 일괄 변경하지 않습니다.

## Source Observation Ownership

`alarm.http`의 관리 통계는 `GET /internal/alarm/count`와 shared `EntryCount` 응답을 사용한다. worker가 방·채널 쌍을 집계하고 admin plane은 전체 알림 목록을 적재하지 않는다. provider를 먼저 배포한 뒤 consumer를 배포한다.

Source observation의 공용 envelope·canonical JSON·hash·lease 계약은 `hololive/hololive-shared/pkg/contracts/sourceobservation`이 소유합니다. Collector의 순수 수집 계약은 `internal/runtime/collection`, lease SQL은 `joblease`, publish/checkpoint는 `sourceobservation`이 소유합니다. API `internal/youtube/sourceobservation`은 consume/replay/retention을, `internal/youtube/canonicalwrite`는 transaction 전용 canonical 저장을 소유합니다. 실제 양 구현을 연결하는 교차 DB 시험은 module-root testkit을 사용하며, upcoming candidate의 canonical clock 시험은 worker `internal/service/alarm/dispatchoutbox`에 있습니다. [Canonical JSON v1](contracts/source-observation-canonical-json-v1.md).

## Runtime DB acquisition capacity (D09)

`hololive-shared/pkg/providers/database`의 runtime factory는 shared-go `pgxdb.AcquirePolicy`를 명시적으로 설정합니다. `bot`, `youtube_plane`, `alarm_worker`, `collector`는 reserve 1, `admin`과 `llm_scheduler`는 reserve 0이며 모두 획득 예산은 5초입니다. reserve 1의 max는 2 이상이어야 하고 min 0은 유효합니다. worker goroutine 수를 연결 수와 합산하지 않습니다. `ProvideMaintenanceDatabaseResources`, replay CLI와 db-migrate는 nil policy를 유지합니다.

일반 획득은 물리 연결을 요청하기 전 `MaxConns-ReservedConns` 한도를 따릅니다. Exec·Query·QueryRow·Begin/BeginTx·Batch·CopyFrom·Ping 및 명시적 Acquire가 같은 pool 경로를 거치며, rows/transaction의 연결 반환까지 permit을 유지합니다. 5초는 gate와 물리 pool/연결 준비 대기를 합친 예산이고 SQL 실행 timeout이 아닙니다. 더 이른 부모 deadline, SQLSTATE, commit 결과 불명 및 취소 독립 rollback 5초는 바꾸지 않습니다.

control 표시는 다음 짧은 DB 경계에서만 사용합니다.

- **bot:** inbox/command heartbeat 한 번, poison/이미 처리된 inbox 완료·release/defer, command+inbox 결과 정산, reply acceptance 기록과 결과 정산. command/provider 실행·입장·claim·maintenance는 일반 획득입니다.
- **source observation:** 기존 `withBudgetDB` 예산 안의 claim budget 갱신+canonical/finalize 한 transaction, retry/dead-letter 정산 및 같은 retry 경로의 종료 release. consume callback은 DB transaction 전용이며 provider 호출이 없습니다. 기존 source DB 슬롯과 슬롯 대기를 포함한 timeout은 유지합니다. claim·queue snapshot·projection refresh·live-end sweep·retention·replay는 일반 획득입니다.
- **alarm worker:** alarm dispatch의 5초 state context(MarkSending, 성공/실패/격리/해제·request generation 정산), notification의 bounded sending/실패/격리/소진 정산 및 취소 독립 최종 정산, YouTube delivery의 bounded BeginSending/prepared failure와 취소 독립 provider 결과·request generation 정산. admission·fanout·request 준비·조회·maintenance·실제 provider 호출은 일반 경로입니다.
- **collector:** lease renewal 한 번, 기존 supervision 경계의 fenced release/defer, empty completion, 기존 publish 예산 안의 observation/checkpoint+lease terminal transaction. 후보 조회·lease 획득·input 조회·provider 대기·수집에는 표시하지 않습니다. admitted execution deadline은 publish까지 계속 적용하고 기존 독립 cleanup 예산과 provider gate join 규칙은 유지합니다.

runtime의 raw `pgx.Connect`, `Conn.Hijack`, `AcquireAllIdle` 사용은 없습니다. migration 전용 연결과 nil-policy 테스트 pool의 Hijack은 runtime 보호 범위가 아닙니다. transaction callback은 받은 tx를 재사용하며, commit 결과 재조회는 Commit이 물리 연결을 반환한 뒤 수행합니다. 정책 pool에 bypass API를 추가하면 별도 소유권 검토가 필요하며 generic 지원으로 간주하지 않습니다.

`hololive_db_pool_*`의 pgx Stat wait는 native pool 대기만 측정하고 바깥 gate 대기를 포함하지 않습니다. worker deadline에는 gate 대기가 포함됩니다. 기존 API process 공유 Valkey/member cache와 backing bot pool의 종료 소유권은 바꾸지 않습니다. 이 reserve는 DB/network 장애나 제어 작업끼리의 경합을 없애지 않으며, 운영 활성화 증거는 별도입니다.


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
