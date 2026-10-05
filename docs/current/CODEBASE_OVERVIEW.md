# Codebase Overview

이 문서는 `hololive-bot` 코드베이스를 처음 보는 개발자가 전체 구조와 주요 실행 경로를 빠르게 파악하기 위한 온보딩 문서입니다. 운영 인벤토리의 정본은 `PROJECT_MAP.md`, 책임 경계의 정본은 `SERVICE_OWNERSHIP.md`, 배포 기준의 정본은 `DEPLOYMENT_BASELINE.md`입니다.

## 한 줄 요약

`hololive-bot`은 Go 중심 모노레포입니다. Kakao/Iris 봇 ingress, 알람 처리, YouTube collector AP fleet, LLM 스케줄링, 관리자 API, 공유 라이브러리를 `hololive-api` 통합 런타임(bot/admin/llm/YouTube plane), `alarm-worker`, `youtube-collector`가 소유합니다. 중앙과 Seoul collector `b`는 Docker Compose, Osaka collector `a`와 Osaka2 collector `d`는 native systemd로 운영합니다. 관리자 웹은 iris-seoul의 `iris-console.service`가 제공합니다.

## 큰 구조

```text
.
├── hololive/
│   ├── hololive-api/               # Unified bot/admin/llm/YouTube consume runtime
│   ├── hololive-alarm-worker/      # Alarm checker, dispatch queue, proactive egress
│   ├── hololive-youtube-collector/  # AP-fleet YouTube collector module
│   ├── hololive-shared/            # shared domain, config, providers, contracts, services
│   └── hololive-dbtest/            # PostgreSQL test harness and migration replay
├── admin-dashboard/                # web ownership boundary guidance
├── docs/current/                   # current architecture, service, contract, runbook docs
├── scripts/                        # architecture, deploy, log, runtime, CI helpers
└── deploy/compose/                 # Docker Compose baselines and overlays
    ├── docker-compose.prod.yml     # production compose baseline
    ├── docker-compose.live-compat.yml # main-host live wiring overlay (always applied; owns collector-c ports/volumes)
    ├── docker-compose.seoul.yml    # Seoul Compose AP (youtube-collector-b + youtube-po-b)
    ├── docker-compose.osaka.yml    # native Osaka a configuration/path validation
    └── docker-compose.osaka2.yml   # native Osaka2 d configuration/path validation
```

`go.work` ties the root module, the Go runtime/shared/dbtest modules under `hololive/`, and sibling modules `../shared-go` and `../iris-client-go` together. The three production runtime binaries (`hololive-api`, `alarm-worker`, `youtube-collector`) are implemented in Go 1.27.x. Web implementation, credentials, SSR and native deployment belong to sibling `../iris-console`; `admin-dashboard/` contains the Hololive web boundary guidance.

## Runtime Services

The current production runtime set is three Go binaries:

| Runtime | Path | Main responsibility | Typical port |
|---|---|---|---:|
| `hololive-api` | `hololive/hololive-api/` | Bot/admin/llm planes plus YouTube consume/canonical persist | 30001/30003/30006 |
| `alarm-worker` | `hololive/hololive-alarm-worker/` | Alarm checks, queue consumption, proactive notification egress | 30007 |
| `youtube-collector` | `hololive/hololive-youtube-collector/` | AP fleet fetch/normalize/lease/Publish (`a`/`b`/`c`/`d`) | 30005/30015/30025/30035 |

## Shared Libraries

`hololive/hololive-shared/` is the central shared module. It contains:

- domain models under `pkg/domain`;
- config section types, loaders and pure validators under `pkg/config` (runtime aggregates belong to each module's `internal/config`);
- typed infra constructors under `pkg/providers/{cache,database,holodex,iris,member,modules}`;
- shared service implementations under `pkg/service`;
- runtime contracts under `pkg/contracts`;
- database/cache integration under internal/shared packages.

실행 구현의 조립은 각 module이 소유합니다. API는 `internal/config`와 plane별 인스턴스를 만드는
`internal/apifoundation`, admin의 `runtime`·`internal/httpapi`, 공통 trigger의 `internal/httpapi`를 사용합니다.
Observation publish는 collector `internal/runtime/sourceobservation`, consume와 private reducer는 API
`internal/youtube/{sourceobservation,reconcile,community}`에 있습니다. Worker의 구독 서비스·private cache,
dedup·queue·dispatchoutbox, alarm dispatch runner·formatter와 범용 notification delivery dispatcher·store·SQL
(`internal/egress/notificationdelivery`)은 worker의 `internal/`이 소유합니다.
공용 strict 환경 파싱과 순수 정책은 shared `pkg/config/{envload,runtimepolicy}`, 설정 구획 타입·loader·순수 검증은
`pkg/config/settings`, 시간과 target-minute 계산은 `pkg/timeutil`·`pkg/alarmtiming/targetpolicy`, DB 구성은
`pkg/providers/database`가 담당합니다. API `BotPlaneConfig`/`AdminPlaneConfig`/`APIWorkerProfile`과 worker
`RuntimeConfig`/`AlarmWorkerProfile`은 각 module `internal/config`가 조립합니다.

Sibling `../shared-go/` holds lower-level utilities shared outside the Hololive-specific modules.

## Core Data Flow

### Kakao Command Flow

```text
Kakao / Iris
  -> hololive-api (bot plane)
  -> command router / service clients
  -> PostgreSQL / Valkey / hololive-api llm plane / alarm APIs as needed
  -> Kakao / Iris response
```

The `hololive-api` bot plane owns webhook ingress and user-facing command routing. It must not take over alarm scheduling loops, proactive dispatch consumption, or shift admin/llm responsibilities outside their planes.

### YouTube Collection Flow

```text
youtube-collector AP fleet
  -> Holodex / Official / YouTube.js fetch/normalize
  -> PostgreSQL source_observations Publish
hololive-api YouTube plane
  -> observation consume + canonical persist + notification intent
  -> alarm-worker
  -> room resolution, rendering, retry, delivery rows
  -> Iris / Kakao egress
```

YouTube notifications require the collector fleet and the `hololive-api` YouTube plane consumer. `alarm-worker` owns final delivery. Duplicate suppression depends on observation identity, PostgreSQL collection fences, and the dispatch worker's delivery claims.

### LLM Work Flow

```text
hololive-api bot/admin planes / scheduled runtime
  -> hololive-api llm plane internal contracts
  -> PostgreSQL / Valkey / cliproxy or LLM provider
  -> summarized result or scheduled delivery
```

The `hololive-api` llm plane owns major event and member-news scheduling. Other runtimes and planes should call documented contracts instead of importing internal packages.

### Config / Queue / Coordination Flow

```text
runtime services
  -> owning API/worker/collector config loader + shared strict/policy leaves
  -> PostgreSQL and Valkey
  -> member epoch Pub/Sub / alarm wakeup / runtime cache
```

`youtube-collector` scheduling uses PostgreSQL leases. It does not join this Valkey Pub/Sub or cache path.

내부 HTTP/H3 client에는 시작 시 적재한 옵션을 명시적으로 전달합니다. API bot·admin의 alarm client는
worker HTTP provider를 사용합니다. `alarmAdvanceMinutes` 공개 입력 범위는 `1..1440`이며, 저장 실패 때
이전 Get·disk 값과 worker 미호출을 유지합니다. 적용 뒤 응답 유실은 결과 불명으로 보고하며 자동 재시도나 파일 rollback을 하지 않습니다.

Queue and Pub/Sub behavior should be checked against `QUEUE_AND_PUBSUB_CONTRACTS.md` and `CONTRACT_MAP.md` before changing producers or consumers.

## Deployment Model

현재 production의 실행 경로는 다음과 같습니다. 모든 컴파일·테스트·이미지 빌드는 kapu에서 수행하고, 런타임 호스트에는 검증한 image/native bundle을 전송합니다.

| 대상 | 실행 방식·설정 | 배포·검증 절차 |
|---|---|---|
| 중앙 API·worker·collector `c`·infra | `deploy/compose/docker-compose.prod.yml` + `docker-compose.live-compat.yml` | [release runbook](runbooks/release.md); collector `c`와 issuer는 [paired cutover](runbooks/youtube-collector.md#isolated-po-token-lifecycle) |
| Seoul collector `b` | `docker-compose.prod.yml` + `docker-compose.seoul.yml`, issuer `youtube-po-b` | `scripts/deploy/ap-deploy.sh seoul`; [collector runbook](runbooks/youtube-collector.md) |
| Osaka collector `a`·Osaka2 collector `d` | native `hololive-youtube-collector@youtube-collector-{a,d}.service`와 `hololive-youtube-po.service` | `scripts/deploy/ap-host-native-deploy.sh`의 `osaka` 또는 `osaka2` 대상; [collector runbook](runbooks/youtube-collector.md) |
| 통합 관리자 웹 | iris-seoul의 `iris-console.service` | Iris Console 운영 절차; 이 저장소의 [연결 경계](runbooks/admin-dashboard.md) |

`scripts/deploy/ap-hosts/*.conf`의 `AP_RUNTIME_MODE`는 Seoul에 `compose`, Osaka·Osaka2에 `native`를 지정합니다. Osaka의 Compose overlays는 설정·경로 계약 검증용입니다. 상태 확인은 `scripts/logs/`, host별 완료 판정은 `scripts/deploy/ap-completion-check.sh`와 collector runbook을 따릅니다.

Live deploy, restart, rollback, secret writes, and production config mutation require explicit operator approval.

## YouTube Collector Fleet Notes

`youtube-collector` is the four-member AP fleet: Osaka `a` (host-native, 30005), Seoul `b` (Compose, 30015), central unsuffixed `youtube-collector` (`c`, 30025), Osaka2 `d` (host-native, 30035). There is no extra central singleton beyond fleet member `c`. All four members share PostgreSQL collection leases (`hololive_scraper`, `verify-full` TLS). The important invariants are:

- collector owns fetch/normalize/lease/checkpoint/`source_observations` Publish only;
- `hololive-api` YouTube plane owns claim/finalize, canonical persist, notification intent, live-end finalizer, and retention/replay;
- `members.photo` stays on hololive-api admin PhotoSync; YouTube channel photos are the `channel_photo` reducer;
- final notification delivery is owned by `alarm-worker`.

Current operational details live in `docs/current/services/youtube-collector.md` and `docs/current/runbooks/youtube-collector.md`. Dated plans under `docs/current/plans/` and records under `docs/history/` are supporting references, not the operational source of truth.

## Where To Start For Common Tasks

| Task | Start here |
|---|---|
| Find runtime ownership | `docs/current/SERVICE_OWNERSHIP.md` |
| Find module/service inventory | `docs/current/PROJECT_MAP.md` |
| Change deploy shape | `docs/current/DEPLOYMENT_BASELINE.md`, `deploy/compose/`, `scripts/deploy/ap-hosts/`, `scripts/deploy/lib/hololive-youtube-collector.service` |
| Release, rollback, or deploy | `docs/current/runbooks/release.md`, `docs/current/runbooks/rollback.md` |
| Change a runtime API contract | `docs/current/CONTRACT_MAP.md`, `docs/current/contracts/`, `hololive/hololive-shared/pkg/contracts/` |
| Change runtime construction or cleanup | `hololive/hololive-api/internal/config/`, `internal/apifoundation/`, `internal/app/`, `internal/fxapp/`; each plane's `runtime/` |
| Change YouTube collection | `hololive/hololive-youtube-collector/`, `docs/current/services/youtube-collector.md`, `docs/current/runbooks/youtube-collector.md` |
| Change Community collection | `hololive/hololive-youtube-collector/`, `docs/current/services/youtube-collector.md`, `docs/current/runbooks/youtube-collector.md` |
| Change final notification delivery | `docs/current/contracts/alarm.md`, `docs/current/QUEUE_AND_PUBSUB_CONTRACTS.md`, `docs/current/runbooks/alarm-worker.md`, `docs/current/runbooks/dlq-replay.md`, `hololive/hololive-alarm-worker/` |
| Change command handling | `hololive/hololive-api/internal/planes/bot/` |
| Change Hololive admin API / web | API: `hololive/hololive-api/internal/planes/admin/`; web: `../iris-console/`; boundary: `admin-dashboard/AGENTS.md`, `docs/current/runbooks/admin-dashboard.md` |
| Run architecture checks | `scripts/architecture/` |
| Run deploy/status checks | `scripts/deploy/`, `scripts/logs/` |

## Verification Commands

Use the smallest command that matches the change. For broad Go runtime changes, the non-deploying baseline is:

```bash
./build-all.sh --no-bump --build-only
go build ../shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/...
go test ../shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/... ./hololive/hololive-dbtest/...
```

Run the deploying `./build-all.sh --no-bump` path only with explicit operator approval because it can recreate live Compose services.

For documentation-only changes, review the diff and verify referenced source/command paths. For implementation changes affecting runtime ownership, use the existing product boundary checks:

```bash
./scripts/architecture/ci-boundary-gate.sh
```

## Practical Rules

- Start from the nearest `AGENTS.md`; subtree rules override broad repository rules.
- Keep service ownership boundaries intact.
- Do not import another service's `internal` package to bypass a contract.
- Keep generated files, runbooks, and compose docs aligned when changing runtime shape.
- Use `slog` for Go logging and avoid logging secrets.
- Prefer small, reversible changes with targeted tests before broad build/test runs.
