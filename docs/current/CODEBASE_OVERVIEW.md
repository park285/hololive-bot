# Codebase Overview

이 문서는 `hololive-bot` 코드베이스를 처음 보는 개발자가 전체 구조와 주요 실행 경로를 빠르게 파악하기 위한 온보딩 문서입니다. 운영 인벤토리의 정본은 `PROJECT_MAP.md`, 책임 경계의 정본은 `SERVICE_OWNERSHIP.md`, 배포 기준의 정본은 `DEPLOYMENT_BASELINE.md`입니다.

## 한 줄 요약

`hololive-bot`은 Go 중심 모노레포입니다. Kakao/Iris 봇 ingress, 알람 처리, YouTube collector AP fleet, LLM 스케줄링, 관리자 API, 공유 라이브러리를 `hololive-api` 통합 런타임(bot/admin/llm plane), `alarm-worker`, `youtube-collector`로 Docker Compose production baseline과 AP overlays에서 운영합니다.

## 큰 구조

```text
.
├── hololive/
│   ├── hololive-api/               # Unified runtime: bot/admin/llm planes (webhook ingress, admin API, LLM/news/event scheduler)
│   ├── hololive-alarm-worker/      # Alarm checker, dispatch queue, proactive egress
│   ├── hololive-youtube-collector/  # AP-fleet YouTube collector module
│   └── hololive-shared/            # shared domain, config, providers, contracts, services
├── shared-go/                      # lower-level shared Go utilities
├── admin-dashboard/                # dashboard frontend/backend assets
├── docs/current/                   # current architecture, service, contract, runbook docs
├── scripts/                        # architecture, deploy, log, runtime, CI helpers
└── deploy/compose/                 # Docker Compose baselines and overlays
    ├── docker-compose.prod.yml     # production compose baseline
    ├── docker-compose.seoul.yml    # Seoul split-host AP (youtube-collector-b)
    └── docker-compose.main-ap.yml  # main-host AP overlay reserved for collector-c live-compat; service remains youtube-collector
```

`go.work` ties the root module, the Go runtime/shared modules under `hololive/`, and `shared-go/` together. The three production runtime binaries (`hololive-api`, `alarm-worker`, `youtube-collector`) are implemented in Go 1.27.x; `admin-dashboard/` contains the dashboard frontend/backend assets outside the Go runtime count.

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
- config loading and validation under `pkg/config`;
- provider wiring under `pkg/providers`;
- shared service implementations under `pkg/service`;
- runtime contracts under `pkg/contracts`;
- database/cache integration under internal/shared packages.

`shared-go/` holds lower-level utilities shared outside the Hololive-specific modules.

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
  -> shared config loader
  -> PostgreSQL and Valkey
  -> settings Pub/Sub / alarm queues / runtime cache
```

`youtube-collector` scheduling uses PostgreSQL leases. It does not join this Valkey Pub/Sub or cache path.

Queue and Pub/Sub behavior should be checked against `QUEUE_AND_PUBSUB_CONTRACTS.md` and `CONTRACT_MAP.md` before changing producers or consumers.

## Deployment Model

The production baseline is Docker Compose, not Kubernetes. The main files are:

- `deploy/compose/docker-compose.prod.yml`: production service shape;
- `deploy/compose/docker-compose.seoul.yml`: Seoul split-host active-active AP (`youtube-collector-b`);
- `deploy/compose/docker-compose.main-ap.yml`: main-host AP overlay reserved for collector-c live-compat; Compose service remains `youtube-collector`;
- `scripts/deploy/`: deployment and compose validation helpers;
- `scripts/logs/`: status and smoke-check helpers;
- `docs/current/runbooks/`: current service runbooks (`youtube-collector.md` is the YouTube collect runtime);
- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`: historical Compose procedure (retired `youtube-producer` names may remain there).

Live deploy, restart, rollback, secret writes, and production config mutation require explicit operator approval.

## YouTube Collector Fleet Notes

`youtube-collector` is the four-member AP fleet: Osaka `a` (host-native, 30005), Seoul `b` (Compose, 30015), central unsuffixed `youtube-collector` (`c`, 30025), Osaka2 `d` (host-native, 30035). There is no extra central singleton beyond fleet member `c`. All four members share PostgreSQL collection leases (`hololive_scraper`, `verify-full` TLS). The important invariants are:

- collector owns fetch/normalize/lease/checkpoint/`source_observations` Publish only;
- `hololive-api` YouTube plane owns claim/finalize, canonical persist, notification intent, live-end finalizer, and retention/replay;
- `members.photo` stays on hololive-api admin PhotoSync; YouTube channel photos are the `channel_photo` reducer;
- final notification delivery is owned by `alarm-worker`.

Current operational details live in `docs/current/services/youtube-collector.md` and `docs/current/runbooks/youtube-collector.md`. Planning archives under `docs/superpowers/` or `docs/history/` are supporting history, not the operational source of truth.

## Where To Start For Common Tasks

| Task | Start here |
|---|---|
| Find runtime ownership | `docs/current/SERVICE_OWNERSHIP.md` |
| Find module/service inventory | `docs/current/PROJECT_MAP.md` |
| Change deploy shape | `deploy/compose/docker-compose.prod.yml`, `deploy/compose/docker-compose.seoul.yml`, `docs/current/DEPLOYMENT_BASELINE.md` |
| Release, rollback, or deploy | `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`, `docs/current/runbooks/release.md`, `docs/current/runbooks/rollback.md` |
| Change a runtime API contract | `docs/current/CONTRACT_MAP.md`, `docs/current/contracts/`, `hololive/hololive-shared/pkg/contracts/` |
| Change YouTube collection | `hololive/hololive-youtube-collector/`, `docs/current/services/youtube-collector.md`, `docs/current/runbooks/youtube-collector.md` |
| Change Community collection | `hololive/hololive-youtube-collector/`, `docs/current/services/youtube-collector.md`, `docs/current/runbooks/youtube-collector.md` |
| Change final notification delivery | `docs/current/contracts/alarm.md`, `docs/current/QUEUE_AND_PUBSUB_CONTRACTS.md`, `docs/current/runbooks/alarm-worker.md`, `docs/current/runbooks/dlq-replay.md`, `hololive/hololive-alarm-worker/` |
| Change command handling | `hololive/hololive-api/internal/planes/bot/` |
| Change admin dashboard API | `hololive/hololive-api/internal/planes/admin/`, `admin-dashboard/` |
| Run architecture checks | `scripts/architecture/` |
| Run deploy/status checks | `scripts/deploy/`, `scripts/logs/` |

## Verification Commands

Use the smallest command that matches the change. For broad Go runtime changes, the non-deploying baseline is:

```bash
./build-all.sh --no-bump --build-only
go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/...
go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-youtube-collector/...
```

Run the deploying `./build-all.sh --no-bump` path only with explicit operator approval because it can recreate live Compose services.

For architecture-doc changes, prefer:

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-contract-map.sh
./scripts/architecture/ci-boundary-gate.sh
```

## Practical Rules

- Start from the nearest `AGENTS.md`; subtree rules override broad repository rules.
- Keep service ownership boundaries intact.
- Do not import another service's `internal` package to bypass a contract.
- Keep generated files, runbooks, and compose docs aligned when changing runtime shape.
- Use `slog` for Go logging and avoid logging secrets.
- Prefer small, reversible changes with targeted tests before broad build/test runs.
