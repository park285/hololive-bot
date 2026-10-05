# Service Ownership

## Scope

현재 3개 app runtime(`hololive-api`, `alarm-worker`, `youtube-collector`)의 책임 경계와 금지 소유 범위를 정리합니다. `hololive-api`는 bot/admin/llm/YouTube plane을 한 프로세스에서 호스팅합니다. Historical handoff 문서는 `docs/history/runtime-split/`에 보관합니다.

## Ownership Matrix

| Runtime | Owns | Provides | Consumes | Must not own | Detail |
|---|---|---|---|---|---|
| `hololive-api` | Bot plane: Kakao/Iris webhook ingress, command routing, user-facing replies. Admin plane: dashboard-facing admin HTTP control plane + `members.photo` Holodex PhotoSync product path. LLM plane: major event/member news scheduling, LLM summaries, internal subscription/trigger APIs. YouTube plane: observation claim/finalize, canonical persist, notification intent, live-end finalizer, retention/replay | Kakao webhook/H3 ingress, Admin API + trigger client, `membernews`/`majorevent`/`trigger` internal HTTP contracts, YouTube consume | PostgreSQL (`hololive_runtime`), Valkey, Iris, worker alarm HTTP API through bot/admin clients, cliproxy/LLM where configured | alarm checking worker, alarm scheduling loops, proactive dispatch queue consumption, proactive notification egress, collector scrape/lease | `services/hololive-api.md` |
| `alarm-worker` | Alarm HTTP provider, alarm checker, alarm scheduler, dispatch queue publishing/consumption, proactive notification egress | Alarm HTTP provider, alarm queue publisher/consumer, YouTube outbox dispatcher | PostgreSQL, Valkey, `settings.json`, Iris | Kakao command routing, YouTube collection, YouTube canonical detection write | `services/alarm-worker.md` |
| `youtube-collector` | AP fleet (`a`/`b`/`c`/`d`) external clients, provider adapters, fixture-backed parsing, normalization, collection target read, DB job lease/fence, bounded scheduling, rate limit/retry/cooldown, checkpoint, observation publish, provider health | Community/content/live/stats/profile/photo/schedule observations for the `hololive-api` YouTube plane | PostgreSQL (`hololive_scraper`) | canonical tables, live transition, domain watermark, notification intent/outbox, profile/photo 최종 선택, proactive egress | `services/youtube-collector.md` |

## Split Rules

- Cross-service APIs must use documented contracts under `docs/current/contracts/` and `hololive/hololive-shared/pkg/contracts/*`.
- Service-to-service `internal` package imports are not allowed as an ownership shortcut.
- Queue/PubSub changes must update `CONTRACT_MAP.md`, `QUEUE_AND_PUBSUB_CONTRACTS.md`, and affected service docs.
- Unclear ownership is marked `검토 필요` in the service doc instead of being silently assigned.
- Runtime binaries use their owning loaders: API `internal/config.LoadRuntime`, worker `internal/config.LoadRuntime`, collector `internal/config.LoadRuntime`. API plane settings and worker profile ownership are validated before queues or egress clients are constructed. Collector `--check-worker-profile` applies the same profile-owned numeric policy as runtime `Config.Validate` without reading DB or provider env.
- API `internal/apifoundation` creates common services with the consuming plane's options and cache; it does not merge plane instances or bounded DB pools. Internal H3 options are passed from loaded config, while each plane owns client timeout and transport cleanup.
- API planes register acquired resources immediately for rollback and transfer the same owner to a successful runtime. `CloseContext` receives the remaining process-stop budget, joins background tasks including durable samplers, then releases member cache before PG/cache. A join timeout preserves live resources and the error; it does not launch concurrent duplicate cleanup.
- Cross-module behavior tests use narrow module-root testkits (`hololive-api/testkit/sourceobservation`, `hololive-youtube-collector/testkit/sourceobservation`, `hololive-alarm-worker/alarmtestkit`) to run the actual publisher, consumer or worker provider without importing a peer's `internal` package.

## Shared Package Retention

`hololive-shared/pkg`는 외부 안정 API 전체가 아니라 monorepo 내부 cross-runtime 계약면입니다. 단일 runtime만 소비하는 실행 구현은 해당 module의 `internal/`로 이동하지만, 다음 범주는 shared에 남습니다.

- Cross-runtime 계약·domain 값: `pkg/contracts/*`(YouTube 저장 payload는 `pkg/contracts/youtubeoutbox`), `pkg/domain`, alarm HTTP client/DTO/handler/route registrar와 repository primitives인 `pkg/service/alarm`.
- 공통 기반: `pkg/config/{envload,runtimepolicy,settings}`, `pkg/timeutil`, `pkg/alarmtiming/targetpolicy`, `pkg/providers/{cache,database,holodex,iris,member,modules}` 및 DB/cache/member/delivery/template·HTTP 서버 기반. `pkg/config/settings`는 설정 구획 타입·loader·순수 검증만 소유하며, runtime 설정 집계와 worker profile 조립은 API·worker `internal/config`가 소유합니다. shared `pkg/service/delivery`는 producer enqueue 저장소, 요청·결과·sender interface, Iris transport와 locker만 둡니다. DB factory는 설정을 DB options로 변환하며 순수 `pkg/service/database`에 startup settings 의존을 넣지 않습니다.
- YouTube 공통 라이브러리: `pkg/service/youtube/outbox/{analytics,telemetry,deliverysql,timeline}`, `tracking/observation`, `contentid`, `timestamp`. API만 쓰는 canonical 저장은 API `internal/youtube/canonicalwrite`의 transaction 전용 함수가 소유합니다. 호출자가 없던 HTML/RSS scraper·parser·admission·YouTube 분산 limiter는 제거했으며, collector의 로컬 요청 pacing은 `internal/runtime/youtubejs`에 있습니다.
- Worker 전용 구현은 `hololive-alarm-worker/internal/config`, `internal/service/alarm/{subscriptions,dedup,dispatchoutbox,queue}`, `internal/egress/youtubedispatch/format`으로 회수했습니다. private alarm cache는 `subscriptions/internal/alarmcache`, alarm dispatch runner와 SQL은 `internal/egress/alarmdispatch`, 범용 notification delivery dispatcher·consumer store·maintenance·SQL은 `internal/egress/notificationdelivery`가 소유합니다.
- Collector의 순수 job 계약·target snapshot·수집 입력/결과·retry 값은 `internal/runtime/collection`, lease SQL은 `joblease`, observation publish·checkpoint SQL은 `sourceobservation`이 소유합니다. Provider는 저장 adapter를 import하지 않습니다. API의 consume·canonical·replay·retention과 private reducer는 `internal/youtube/{sourceobservation,canonicalwrite,reconcile,community}`가 소유합니다. 공용 envelope·canonical JSON·hash·lease 값 계약은 shared `pkg/contracts/sourceobservation`에 남습니다.
- 퇴역 producer의 poll scheduler(`pkg/service/youtube/poller/runtime/scheduler`)와 budget·job claim 타입, v3 handoff(`pkg/service/alarm/handoff`)는 해당 퇴역 계약에 따라 삭제했습니다. 이전 shared 분리 단계의 경로를 현재 구현 owner로 사용하지 않습니다.
- Observation publisher와 consumer는 서로 import하지 않으며 같은 테이블을 각자의 DB role 권한으로 다룹니다. 실제 발행·소비 교차 시험은 각 module-root testkit을 사용합니다. 테스트 소비가 있다는 이유로 단일 runtime의 실행 구현을 shared에 남기지 않습니다.
- YouTube outbox formatter의 `MemberNameSource`는 메시지마다 PostgreSQL 표시명 정본을 조회합니다. Valkey 이름 cache를 정본으로 사용하지 않으며, 조회 오류와 성공한 빈 결과는 [alarm 계약](contracts/alarm.md)의 서로 다른 경로를 따릅니다.

YouTube dispatcher와 수집 실행처럼 단일 owner로 확정된 코드는 각각 `hololive-alarm-worker/internal/egress/youtubedispatch`와 `hololive-youtube-collector/internal/runtime/collectorruntime`이 소유합니다. public package 잔류는 구현 ownership을 공유한다는 뜻이 아니며, 새 single-owner 실행 구현을 `hololive-shared/pkg`에 추가할 근거로 사용할 수 없습니다.

## Validation

```bash
go test ./hololive/hololive-api/internal/config ./hololive/hololive-alarm-worker/internal/config \
  ./hololive/hololive-youtube-collector/internal/config
go test ./hololive/hololive-api/internal/youtube/sourceobservation \
  ./hololive/hololive-youtube-collector/internal/runtime/sourceobservation \
  ./hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox
```
