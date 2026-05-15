# Hololive Bot

홀로라이브 VTuber 알림/관리 플랫폼입니다. KakaoTalk 챗봇을 통해 방송 알림, 스트림 상태, 멤버 뉴스, 운영 관리 기능을 제공합니다.

이 README는 저장소 진입점입니다. 현재 구조의 상세 SSOT는 [docs/current/PROJECT_MAP.md](docs/current/PROJECT_MAP.md)이며, 배포 절차는 [docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md](docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md)를 따릅니다.

## Architecture

현재 운영 기준은 Go runtime 6개를 단일 호스트 Docker Compose로 실행하는 구조입니다.

> 운영 기준: 2026-03-07 k8s/k3s 배포에서 단일 호스트 Docker Compose 기준으로 롤백했습니다. 현재 배포, 로그 조회, 장애 대응 절차의 기준은 Compose 문서와 `docs/current` 문서군입니다.

| Runtime | Module | Compose service | Port | Role |
|---|---|---|---:|---|
| `bot` | `hololive-kakao-bot-go` | `hololive-bot` | 30001 | Kakao/Iris webhook ingress, command routing |
| `admin-api` | `hololive-admin-api` | `hololive-admin-api` | 30006 | Admin HTTP control plane |
| `alarm-worker` | `hololive-alarm-worker` | `hololive-alarm-worker` | 30007 | Alarm checker, dispatch queue consumer, and Iris/Kakao proactive egress |
| `llm-scheduler` | `hololive-llm-sched` | `llm-scheduler` | 30003 | Major event, member news, LLM scheduling and delivery |
| `stream-ingester` | `hololive-stream-ingester` | `stream-ingester` | 30004 | Photo sync and ingestion-adjacent runtime |
| `youtube-scraper` | `hololive-stream-ingester` | `youtube-scraper` | 30005 | YouTube scraping/polling and outbox runtime |

Shared libraries:

| Module | Role |
|---|---|
| `hololive-shared` | Hololive domain, contracts, shared services |
| `shared-go` | In-repo Go utilities |

기본 흐름:

- Kakao/Iris ingress: `Iris -> bot -> command/service/repository -> PostgreSQL/Valkey`
- Alarm dispatch: `alarm-worker -> Valkey alarm:dispatch:queue -> alarm-worker egress -> Iris -> KakaoTalk`
- LLM/member news: `admin-api` 또는 `bot` 내부 client -> `llm-scheduler` internal HTTP API
- YouTube ingestion: `youtube-scraper -> shared outbox/tracking -> alarm-worker`

## Development

### Prerequisites

- Go 1.26.3
- PostgreSQL
- Valkey
- Docker Compose for production-like local checks

### Build

```bash
go work sync
go build ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-admin-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...
```

### Test

```bash
go test ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-admin-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...
```

Runtime split contract check:

```bash
go test . -run TestRuntimeSplitStandaloneModulesContract
```

Architecture/doc gates:

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/ci-boundary-gate.sh
```

Local CI gate:

```bash
./scripts/ci/local-ci.sh
```

`./build-all.sh`는 Docker Compose build 전에 이 local CI gate를 실행합니다. gate가 실패하면 image build/deploy 시작 전에 중단됩니다. 기본 gate는 architecture gates, Go toolchain pin, `go work sync` drift, `gofmt`, `go fix` drift, `go mod tidy -diff`, `go vet`, `staticcheck`, `go build`, `go test -count=1`, `govulncheck`를 포함합니다. PostgreSQL integration test까지 포함하려면 `TEST_DATABASE_URL`을 설정하고, race detector까지 포함하려면 `RUN_RACE_TESTS=true`를 설정합니다.

## Deployment

현재 운영 기준은 Docker Compose입니다.

```bash
./build-all.sh --no-bump
./scripts/deploy/compose-redeploy-service.sh hololive-bot
./scripts/deploy/compose-redeploy-service.sh hololive-admin-api
./scripts/deploy/compose-redeploy-service.sh hololive-alarm-worker
./scripts/deploy/compose-redeploy-service.sh llm-scheduler
./scripts/deploy/compose-redeploy-service.sh stream-ingester
./scripts/deploy/compose-redeploy-service.sh youtube-scraper
```

세부 기준:

- [docs/current/PROJECT_MAP.md](docs/current/PROJECT_MAP.md)
- [docs/current/DEPLOYMENT_BASELINE.md](docs/current/DEPLOYMENT_BASELINE.md)
- [docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md](docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md)

## Logs And Health

SSOT는 application stdout/stderr와 `docker compose logs`입니다. 파일 로그는 보조 미러입니다.

| Runtime | Health |
|---|---|
| `bot` | `https://127.0.0.1:30001/health` |
| `admin-api` | `http://127.0.0.1:30006/health` |
| `alarm-worker` | `http://127.0.0.1:30007/health` |
| `llm-scheduler` | `http://127.0.0.1:30003/health` |
| `stream-ingester` | `http://127.0.0.1:30004/health` |
| `youtube-scraper` | `http://127.0.0.1:30005/health` |

```bash
docker compose -f docker-compose.prod.yml ps
docker compose -f docker-compose.prod.yml logs -f <service>
./scripts/logs/logs.sh query <service> --since 1h --limit 1000
```

## Documents

- [docs/README.md](docs/README.md) - 문서 인덱스
- [docs/current/PROJECT_MAP.md](docs/current/PROJECT_MAP.md) - 현재 runtime/module/operation 인벤토리
- [docs/current/SERVICE_OWNERSHIP.md](docs/current/SERVICE_OWNERSHIP.md) - runtime 소유권
- [docs/current/CONTRACT_MAP.md](docs/current/CONTRACT_MAP.md) - 내부 계약 지도
- [docs/current/runbooks/README.md](docs/current/runbooks/README.md) - runtime runbook 인덱스
