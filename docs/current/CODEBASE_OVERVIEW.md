# Codebase Overview

이 문서는 `hololive-bot` 코드베이스를 처음 보는 개발자가 전체 구조와 주요 실행 경로를 빠르게 파악하기 위한 온보딩 문서입니다. 운영 인벤토리의 정본은 `PROJECT_MAP.md`, 책임 경계의 정본은 `SERVICE_OWNERSHIP.md`, 배포 기준의 정본은 `DEPLOYMENT_BASELINE.md`입니다.

## 한 줄 요약

`hololive-bot`은 Go 중심 모노레포입니다. Kakao/Iris 봇 ingress, 알람 처리, YouTube producer AP, LLM 스케줄링, 관리자 API, 공유 라이브러리를 Docker Compose production baseline과 Osaka split-host override로 운영합니다.

## 큰 구조

```text
.
├── hololive/
│   ├── hololive-kakao-bot-go/      # Kakao/Iris webhook ingress, command routing
│   ├── hololive-admin-api/         # Admin dashboard-facing HTTP API
│   ├── hololive-alarm-worker/      # Alarm checker, dispatch queue, proactive egress
│   ├── hololive-llm-sched/         # LLM/member news/major event scheduler
│   ├── hololive-youtube-producer/   # YouTube producer AP runtime
│   └── hololive-shared/            # shared domain, config, providers, contracts, services
├── shared-go/                      # lower-level shared Go utilities
├── admin-dashboard/                # dashboard frontend/backend assets
├── docs/current/                   # current architecture, service, contract, runbook docs
├── scripts/                        # architecture, deploy, log, runtime, CI helpers
├── docker-compose.prod.yml         # production compose baseline
└── docker-compose.osaka.yml        # Osaka split-host/active-active overrides
```

`go.work` ties the root module, the Go runtime/shared modules under `hololive/`, and `shared-go/` together. The five production runtime binaries are implemented in Go 1.26.x; `admin-dashboard/` contains the dashboard frontend/backend assets outside the Go runtime count.

## Runtime Services

The current production runtime set is five Go binaries:

| Runtime | Path | Main responsibility | Typical port |
|---|---|---|---:|
| `bot` | `hololive/hololive-kakao-bot-go/` | Kakao/Iris webhook ingress, command routing, user-facing replies | 30001 |
| `admin-api` | `hololive/hololive-admin-api/` | Admin dashboard control plane | 30006 |
| `alarm-worker` | `hololive/hololive-alarm-worker/` | Alarm checks, queue consumption, proactive notification egress | 30007 |
| `llm-scheduler` | `hololive/hololive-llm-sched/` | Major event/member news scheduling and LLM-backed work | 30003 |
| `youtube-producer` | `hololive/hololive-youtube-producer/` | YouTube polling/scraping, YouTube outbox production, Holodex photo sync | 30005 |

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
  -> bot
  -> command router / service clients
  -> PostgreSQL / Valkey / llm-scheduler / alarm APIs as needed
  -> Kakao / Iris response
```

The bot owns webhook ingress and user-facing command routing. It must not take over alarm scheduling loops, proactive dispatch consumption, or admin control-plane ownership.

### YouTube Producer Flow

```text
youtube-producer
  -> primary/backfill YouTube polling and Holodex-backed checks
  -> PostgreSQL youtube_notification_outbox
  -> alarm-worker
  -> room resolution, rendering, retry, delivery rows
  -> Iris / Kakao egress
```

The key ownership split is that `youtube-producer` produces YouTube outbox rows and owns Osaka poll coordination/readiness, while `alarm-worker` owns final delivery. Duplicate suppression depends on Valkey `JobRunGuard`, database identities such as `(kind, content_id)`, and the dispatch worker's delivery claims.

### LLM Work Flow

```text
bot / admin-api / scheduled runtime
  -> llm-scheduler internal contracts
  -> PostgreSQL / Valkey / cliproxy or LLM provider
  -> summarized result or scheduled delivery
```

`llm-scheduler` owns major event and member-news scheduling. Other runtimes should call documented contracts instead of importing internal packages.

### Config / Queue / Coordination Flow

```text
runtime services
  -> shared config loader
  -> PostgreSQL and Valkey
  -> settings Pub/Sub / alarm queues / runtime cache / YouTube poll coordination
```

Queue and Pub/Sub behavior should be checked against `QUEUE_AND_PUBSUB_CONTRACTS.md` and `CONTRACT_MAP.md` before changing producers or consumers.

## Deployment Model

The production baseline is Docker Compose, not Kubernetes. The main files are:

- `docker-compose.prod.yml`: production service shape;
- `docker-compose.osaka.yml`: Osaka split-host and active-active overrides;
- `scripts/deploy/`: deployment and compose validation helpers;
- `scripts/logs/`: status and smoke-check helpers;
- `docs/current/runbooks/`: service-specific runbooks;
- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`: deployment procedure entrypoint.

Live deploy, restart, rollback, secret writes, and production config mutation require explicit operator approval.

## Active-Active YouTube Producer Notes

The Osaka `youtube-producer` active-active path runs multiple AP containers while preserving a producer-only contract. The important invariants are:

- per-channel polling uses Valkey-backed `JobRunGuard`;
- successful polls mark a cooldown instead of simply releasing the lease;
- peer-owned and already-completed jobs skip polling;
- Valkey unavailable in active-active mode is fail-closed;
- final notification delivery is still owned by `alarm-worker`.

Current operational details live in `docs/current/services/youtube-producer.md` and `docs/current/runbooks/youtube-producer.md`. Planning archives under `docs/superpowers/` or `docs/history/` are supporting history, not the operational source of truth.

## Where To Start For Common Tasks

| Task | Start here |
|---|---|
| Find runtime ownership | `docs/current/SERVICE_OWNERSHIP.md` |
| Find module/service inventory | `docs/current/PROJECT_MAP.md` |
| Change deploy shape | `docker-compose.prod.yml`, `docker-compose.osaka.yml`, `docs/current/DEPLOYMENT_BASELINE.md` |
| Release, rollback, or deploy | `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`, `docs/current/runbooks/release.md`, `docs/current/runbooks/rollback.md` |
| Change a runtime API contract | `docs/current/CONTRACT_MAP.md`, `docs/current/contracts/`, `hololive/hololive-shared/pkg/contracts/` |
| Change YouTube producer behavior | `hololive/hololive-youtube-producer/`, `hololive/hololive-shared/pkg/service/youtube/`, `docs/current/services/youtube-producer.md` |
| Change final notification delivery | `docs/current/contracts/alarm.md`, `docs/current/QUEUE_AND_PUBSUB_CONTRACTS.md`, `docs/current/runbooks/alarm-worker.md`, `docs/current/runbooks/dlq-replay.md`, `hololive/hololive-alarm-worker/` |
| Change command handling | `hololive/hololive-kakao-bot-go/` |
| Change admin dashboard API | `hololive/hololive-admin-api/`, `admin-dashboard/` |
| Run architecture checks | `scripts/architecture/` |
| Run deploy/status checks | `scripts/deploy/`, `scripts/logs/` |

## Verification Commands

Use the smallest command that matches the change. For broad Go runtime changes, the non-deploying baseline is:

```bash
./build-all.sh --no-bump --build-only
go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-youtube-producer/...
go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-admin-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-youtube-producer/...
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
