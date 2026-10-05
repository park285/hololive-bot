# Project Map

Module and runtime inventory for the `hololive-bot` workspace.

This file is the current runtime ownership authority. Completed handoffs, incident reports, historical architecture snapshots, and verification captures belong under `docs/history/` and must not define current procedures.

## Module Inventory

| Module | Language | Path | Role | Port |
|--------|----------|------|------|------|
| `hololive-alarm-worker` | Go 1.27 | `hololive/hololive-alarm-worker/` | Alarm checker, dispatch queue consumer, and proactive egress worker | 30007 |
| `hololive-api` | Go 1.27 | `hololive/hololive-api/` | Unified runtime hosting bot/admin/llm planes and the YouTube consume plane | 30001/30003/30006 |
| `hololive-dbtest` | Go 1.27 | `hololive/hololive-dbtest/` | PostgreSQL testcontainers harness and production migration replay support | - |
| `hololive-youtube-collector` | Go 1.27 + collector-owned YouTube.js helper | `hololive/hololive-youtube-collector/` | AP-fleet YouTube collector: Holodex / Official / YouTube.js fetch, normalize, collection lease, checkpoint, and observation Publish. No canonical tables, no notification outbox, no egress | 30005/30015/30025/30035 |
| `hololive-shared` | Go 1.27 | `hololive/hololive-shared/` | Shared Go library (hololive domain, contracts, shared services) | - |
| `shared-go` | Go 1.27 | `../shared-go/` (iris-stack submodule) | Shared Go utilities | - |
| `iris-console` | TypeScript gateway / React SSR | `../iris-console/` (iris-stack submodule) | Unified Iris/ChatBotGo/Hololive app with account scopes and one PWA; host control API `iris-admin` belongs to Iris | 8878 (loopback) |
| `deploy/compose/docker-compose.prod.yml` | YAML | `deploy/compose/docker-compose.prod.yml` | Central production Compose service definitions; used with the live-compat overlay | - |
| `deploy/compose/docker-compose.live-compat.yml` | YAML | `deploy/compose/docker-compose.live-compat.yml` | Required central host wiring and shortlink ingress | - |
| `deploy/compose/docker-compose.osaka.yml` | YAML | `deploy/compose/docker-compose.osaka.yml` | Osaka split-host AP overlay (`youtube-collector-a`, host `<tailnet-osaka-a>`) for compose-path contract validation; live runtime is host-native `systemd` | - |
| `deploy/compose/docker-compose.osaka2.yml` | YAML | `deploy/compose/docker-compose.osaka2.yml` | Osaka second split-host AP overlay (`youtube-collector-d`, host `<tailnet-osaka2-d>`) for compose-path contract validation; live runtime is host-native `systemd` | - |
| `deploy/compose/docker-compose.seoul.yml` | YAML | `deploy/compose/docker-compose.seoul.yml` | Seoul Compose AP (`youtube-collector-b` + isolated issuer `youtube-po-b`) | - |

## Runtime Operations Inventory

| Runtime | Module | Binary | Execution target | Port | Health / Ready | Service doc | Runbook |
|---|---|---|---|---:|---|---|---|
| `hololive-api` | `hololive-api` | `hololive-api` | Central Compose `hololive-api` | 30001/30003/30006 | `https://127.0.0.1:30001/health` | `services/hololive-api.md` | `runbooks/hololive-api.md` |
| `alarm-worker` | `hololive-alarm-worker` | `alarm-worker` | Central Compose `hololive-alarm-worker` | 30007 | `https://127.0.0.1:30007/health` | `services/alarm-worker.md` | `runbooks/alarm-worker.md` |
| `youtube-collector` | `hololive-youtube-collector` | `youtube-collector` | Central Compose `youtube-collector` (`c`), Seoul Compose `youtube-collector-b`, Osaka/Osaka2 native systemd `a`/`d` | 30005/30015/30025/30035 | `https://127.0.0.1:30025/ready` (central `c`; 원격 AP는 각 호스트 로컬 H3 `/ready`) | `services/youtube-collector.md` | `runbooks/youtube-collector.md` |

AP execution mode is owned by `scripts/deploy/ap-hosts/*.conf`: `seoul=compose`,
`osaka=native`, `osaka2=native`. Native collectors run as
`hololive-youtube-collector@youtube-collector-a.service` and
`hololive-youtube-collector@youtube-collector-d.service`; their Compose overlays are
configuration/path validation assets. The management web runs separately on iris-seoul as
`iris-console.service`; [its runbook](runbooks/admin-dashboard.md) documents the Hololive connection boundary.

## Infra Services

| Compose service | Role | Notes |
|---|---|---|
| `holo-postgres` | PostgreSQL data store | Bridge-networked PostgreSQL; live-compat explicitly publishes `<tailnet-central>:5433` to container `5432`; `ssl=on`; `iris-stack internal CA` server cert mounted read-only from `/etc/stack-secrets/hololive-bot/postgres-tls/` at `/run/hololive-bot/postgres-tls/` |
| `hololive-db-migrate` | Migration bootstrap/apply job | Must complete before app runtime services start; `PGSSLMODE=verify-full` with `postgres-ca.pem` |
| `valkey-cache` | Valkey cache, queue, Pub/Sub | TCP and Unix socket endpoints |
| `youtube-po-c` / `youtube-po-b` | Isolated PO issuer for central / Seoul collectors | Private Unix sockets; native Osaka/Osaka2 use `hololive-youtube-po.service` |
| `admin-dashboard-ingress` | Central shortlink ingress (the web runs on iris-seoul) | Not part of the 3 app runtime set |
| `docker-proxy` | Docker socket proxy | Used only by `deunhealth` (autoheal). Neither dashboard nor `hololive-api` receives access |
| `deunhealth` | Container autoheal | Restarts unhealthy labeled containers |

## Cross-Runtime Contracts

- Contract map: `CONTRACT_MAP.md`
- Service ownership: `SERVICE_OWNERSHIP.md`
- Runtime runbook index: `runbooks/README.md`
- Deployment baseline: `DEPLOYMENT_BASELINE.md`
- YouTube notification split: `youtube-collector` AP fleet owns external fetch/normalize/collection lease/checkpoint/`source_observation` Publish; `hololive-api` YouTube plane owns observation consume, canonical persist, notification intent, live-end finalizer, and retention/replay; `alarm-worker` owns room resolution, rendering, retry, delivery rows, and Iris/Kakao egress.
- Birthday stream split: collector publishes live evidence; `hololive-api` YouTube plane owns live session/end reconciliation; `alarm-worker` resolves recipients from `status='sent'` deliveries of the matching birthday greeting event and relies on the dispatch ledger for late-room convergence.
- X Spaces: alarm-worker의 선택적 Node helper가 무료 웹 내부 API 관측을 수행하고 기존 dispatch ledger로 LIVE 구독 방에 시작 링크를 보낸다. Iris Admin은 후보 세션 제출·상태 화면을, hololive-api는 암호화된 후보 저장을, worker는 검증·승격을 소유한다. 상세 계약은 `services/x-spaces.md`를 따른다.
- X Spaces 연결 복구는 `DEC-20260924-x-spaces-auto-login-fadeout`에 따라 관리 앱의 수동 쿠키 제출과 worker 후보 검증을 사용한다. 과거 자동 로그인 시도 이력은 연결 상태의 읽기 전용 정보로 남긴다.

## Implementation Ownership

아래 경로는 같은 module identity와 세 runtime 안에서의 코드 소유 경계입니다. binary·port·migration·설치 경로는 위 인벤토리를 따릅니다.

| Owner | Current source paths | Responsibility |
|---|---|---|
| API composition | `hololive/hololive-api/internal/config`, `internal/apifoundation` | API plane 설정(`BotPlaneConfig`·`AdminPlaneConfig`)·`APIWorkerProfile`과 plane별 공통 서비스 생성. 각 plane의 인스턴스·DB pool·transport 수명 유지 |
| API admin HTTP | `hololive/hololive-api/internal/planes/admin/runtime`, `internal/planes/admin/internal/httpapi` | admin 조립·수명과 router·admin 전용 Stream/OAuth/WebSocket handler. 제품 API handler는 `internal/planes/admin/internal/server/api` |
| API common HTTP | `hololive/hololive-api/internal/httpapi` | bot·LLM이 사용하는 trigger handler와 route 조립 |
| API YouTube | `hololive/hololive-api/internal/youtube/sourceobservation`, `internal/youtube/canonicalwrite`, `internal/youtube/reconcile`, `internal/youtube/community` | observation consume, transaction 전용 canonical persist, replay·retention 및 private reducer |
| Worker | `hololive/hololive-alarm-worker/internal/config`, `internal/service/alarm/{subscriptions,dedup,dispatchoutbox,queue}`, `internal/egress/{alarmdispatch,notificationdelivery}`, `internal/egress/youtubedispatch/format` | worker `RuntimeConfig`·`AlarmWorkerProfile`, 구독 서비스·private cache·dispatch 저장/발행·runner, 범용 notification delivery dispatcher·store·SQL, 실제 formatter |
| Collector | `hololive/hololive-youtube-collector/internal/config`, `internal/runtime/{collection,joblease,sourceobservation,youtubejs,youtubejscollector}` | collector 설정·profile·tracing slot 정책, 순수 수집 계약, lease/checkpoint/observation publish, helper RPC·로컬 pacing·pagination |
| Shared foundation | `hololive/hololive-shared/pkg/config/{envload,runtimepolicy,settings}`, `pkg/timeutil`, `pkg/alarmtiming/targetpolicy`, `pkg/providers/{cache,database,holodex,iris,member,modules}` | strict 환경 파싱, 설정 구획 타입·loader·순수 검증, 순수 정책·시간 계산, typed infra 생성자. runtime 설정 집계는 각 module `internal/config`가 소유 |

Alarm HTTP provider는 worker이며 API bot·admin plane은 `ALARM_INTERNAL_URL`의 client입니다. 내부 HTTP/H3 client는 시작 시 적재한 `BotPlaneConfig.InternalH3`·`AdminPlaneConfig.InternalH3` 옵션을 명시적으로 받고, transport와 timeout은 각 plane이 소유합니다. Iris URL 파일의 동적 reload는 별도 기존 경로를 유지합니다.

## Maintenance

- Non-secret host addresses are owned by `deploy/topology/hosts.conf`. Explicit AP, Compose, Nginx,
  nftables, and PostgreSQL HBA consumers are checked by `scripts/architecture/check-topology-parity.sh`;
  update the owner and every affected consumer in one change.
- Keep Go module entries aligned with `go.work`.
- Keep runtime binary and Docker Compose service entries aligned with `deploy/compose/docker-compose.prod.yml`.
- Keep service docs and runbook links valid for all 3 runtime rows.
- Keep contract docs aligned with `hololive/hololive-shared/pkg/contracts/*`.
- Run `./scripts/architecture/ci-boundary-gate.sh` for architecture-wide changes.
- Architecture: Go single-language runtime (3 app runtimes: hololive-api + alarm-worker + youtube-collector AP fleet). `hololive-api` hosts the bot/admin/llm planes in one process on ports 30001/30003/30006.
- Central startup uses `docker-compose.prod.yml` + `docker-compose.live-compat.yml` and starts `hololive-api`, `hololive-alarm-worker`, fleet member `c` (`youtube-collector`, 30025), its isolated issuer and central infra. Runtime hosts use verified images with no builds, following the release runbook.
- Seoul `b` uses `docker-compose.prod.yml` + `docker-compose.seoul.yml`; the AP overlay keeps central app services in `central-only` and runs `youtube-collector-b` with `youtube-po-b`. Osaka `a` and Osaka2 `d` use the native systemd deployment path. [Deployment Baseline](DEPLOYMENT_BASELINE.md) owns the host-level summary.
- Retired runtime names: `hololive-alarm`, `hololive-scraper`, `rust-dispatcher`, `hololive-admin`, `hololive-rs`, `youtube-producer`.
