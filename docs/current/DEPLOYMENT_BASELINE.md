# Deployment Baseline

## Scope

현재 production baseline은 단일 호스트 `deploy/compose/docker-compose.prod.yml`입니다. 이 문서는 runtime/infra 구성의 요약 기준이며, 실제 배포 절차는 `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`를 따릅니다.

## Non-Goals

- k8s/k3s 재도입 설계
- Docker Compose 절차 중복
- service env 전체 목록 복제

## Host Topology

| 역할 | 호스트 | 내용 |
|---|---|---|
| 중앙 런타임 (primary) | `<tailnet-central>` (`aarch64`) | `hololive-api`, `alarm-worker`, `admin-dashboard`, `holo-postgres`, `valkey-cache`, ingress/proxy, main AP `youtube-producer-c`. 권위 PostgreSQL이 여기 있습니다. |
| Hot standby | `<tailnet-seoul-ap>` (`aarch64`) | `holo-postgres-standby`. 중앙 primary에서 물리 스트리밍 복제를 받는 read-only 복제본이며, 승인된 fencing/route backend가 준비되면 fail-closed controller가 승격합니다. |
| 빌드/제어 | `<build-control-host>` (`x86_64`) | 모든 컴파일·이미지 빌드·테스트. 런타임 호스트는 검증된 배포 파일과 이미지만 받습니다. |
| 원격 AP | Osaka `a`, Seoul `b`, Osaka2 `d` | `a`/`d`는 host-native systemd, `b`는 Compose. |

`<build-control-host>`는 두 가지를 추가로 소유합니다. 첫째, CLIProxy와 observability
스택(Jaeger/OTLP, Prometheus, Loki, Grafana, exporter)이 중앙 데이터 평면 이전 때
의도적으로 남았습니다 — `CLIPROXY_BASE_URL`과 `OTEL_EXPORTER_OTLP_ENDPOINT`가
`<build-control-host>`를 가리키는 것은 이전 누락이 아니라 named exception입니다.
둘째, 같은 호스트의 `holo-postgres`/`valkey-cache`는 **백업 사본**입니다(HA standby가
아닙니다 — 그 역할은 `<tailnet-seoul-ap>`가 맡습니다). `hololive-db-backup.timer` user
unit이 매시 중앙 primary에서 논리 덤프를 받아 전체를 덮어쓰므로 최대 1시간 지연되며,
**유일한 writer가 그 타이머**입니다. 손으로 쓰지 마십시오 — 다음 동기화가 버립니다.
`<build-control-host>`가 `x86_64`라 물리 복제 대상이 될 수 없어 논리 덤프를 씁니다.
이 호스트의 `hololive-compose.service`는 `disabled`로 두어 재부팅이 두 번째 alarm
dispatcher를 띄우지 못하게 합니다. 활성화는 명시적 롤백 결정을 요구합니다.

Hot standby(`<tailnet-seoul-ap>`)는 primary와 같은 `aarch64`라 물리 스트리밍 복제가
가능합니다. 정상 상태와 승격 뒤 restart posture는
`deploy/compose/docker-compose.standby.yml`이 소유합니다. 최초 `pg_basebackup`, controller
설치, fencing/route backend 승인과 재시딩 절차는 `runbooks/postgres-replication.md`가
소유합니다. checked-in `postgres-failover.service`는 dry-run이며, apply drop-in은 구 primary를
영속 격리하는 외부 fencing과 권위 DB endpoint를 전환·검증하는 route hook을 모두 준비한
뒤에만 설치합니다. 비동기 복제이므로 마지막 정상 관측 뒤 전파되지 않은 commit의 RPO는
0으로 보장되지 않으며 자동 failback은 지원하지 않습니다.

호스트마다 달라지는 배포 값은 Compose 기본값이 아니라 각 호스트의
`/etc/stack-secrets/hololive-bot/compose.env`가 소유합니다: `HOLOLIVE_*_PORT_BIND_IP`,
`HOLOLIVE_METRICS_PORT_BIND_IP`(메트릭 리스너를 tailnet에 노출해 중앙 Prometheus가
스크레이프할 수 있게 하는 값), 그리고 `DOCKER_SOCKET_GID`(Docker 그룹 gid는 호스트마다
다릅니다).

## Runtime Services

| Runtime | Compose service | Port | Env groups | Volumes | Depends on |
|---|---|---:|---|---|---|
| `hololive-api` | `hololive-api` | 30001/30003/30006 | app file log, Iris, cache, PostgreSQL, major event, cliproxy | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey, docker-proxy |
| `alarm-worker` | `hololive-alarm-worker` | 30007 | app file log, Iris, cache, PostgreSQL | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey |
| `youtube-producer` | `youtube-producer` | 30005 | app file log, cache, PostgreSQL, scraper, major event, cliproxy | `data`, `logs`, Valkey socket | PostgreSQL, migration, Valkey |

## Infra Services

| Service | Purpose | Current notes |
|---|---|---|
| `holo-postgres` | Primary PostgreSQL | Bridge-networked; live-compat publishes `<tailnet-central>:5433` explicitly to container `5432`; TLS `ssl=on`; server certificate mounted read-only from `/etc/stack-secrets/hololive-bot/postgres-tls/` |
| `holo-postgres-standby` | Physical hot standby / promotion target | Seoul host; recovery role and controller-written PGDATA promotion signal must agree; tailnet bind is explicit opt-in |
| `postgres-failover.service` | Fail-closed promotion controller | checked-in unit is dry-run; apply requires trusted fence and route hooks, durable intent/state markers, post-fence old-primary reprobe |
| `hololive-db-migrate` | Migration job | Runs before app services; uses `PGSSLMODE=verify-full` and `/run/hololive-bot/certs/postgres-ca.pem` |
| `valkey-cache` | Cache, queue, Pub/Sub | TCP and Unix socket, password required |
| `admin-dashboard` | Dashboard frontend | Port 30190, not part of Go runtime count |
| `docker-proxy` | Restricted Docker API proxy | Used instead of mounting the Docker socket directly |
| `deunhealth` | Autoheal sidecar | Restarts unhealthy labeled containers; old-primary fencing disables and stops it before fencing PostgreSQL |

## External Boundaries

| Boundary | Used by | Contract doc |
|---|---|---|
| Iris / Redroid KakaoTalk automation | `hololive-api`, `alarm-worker` | `contracts/iris-boundary.md` |
| PostgreSQL | Most runtime services | schema/migration files under `hololive/hololive-api/scripts/migrations` |
| Valkey | cache, alarm queue, config Pub/Sub | `QUEUE_AND_PUBSUB_CONTRACTS.md` |
| CLIPROXY/OpenAI-compatible LLM proxy | `hololive-api`, `youtube-producer` where configured | 검토 필요 |

## PostgreSQL TLS Baseline

Production PostgreSQL access is certificate-verified end to end. `holo-postgres`
loads a server certificate issued by the `iris-stack internal CA` covering
`holo-postgres`, `host.docker.internal`, `localhost`, the central host name and
its tailnet FQDN, the central tailnet/private/public IPs, and `127.0.0.1`/`::1`.
The certificate is a long-lived static file (valid through 2031-08), not an
auto-renewed short-TTL lease: the `stack-secrets` master owns it at
`hosts/<host>/hololive-bot/postgres-tls/`, `tools/sync-host.sh <host> --apply`
mirrors it to `/etc/stack-secrets/hololive-bot/postgres-tls/`, and Compose mounts
that directory read-only at `/run/hololive-bot/postgres-tls/`. Reissuance and
reload are approval-gated `stack-secrets-operations` work; no agent renews it in
place.

The production client set uses `verify-full` with
`/run/hololive-bot/certs/postgres-ca.pem`: `hololive-api`, `alarm-worker`,
central `youtube-producer`, `youtube-producer-c`,
`hololive-db-migrate`, Seoul `youtube-producer-b`, and staged Osaka APs
`youtube-producer-a`/`youtube-producer-d` when they are rolled out.

Operational evidence from the 2026-06-07 transition showed all 35 TCP
PostgreSQL connections on TLSv1.3 and `0` plaintext TCP connections. One Unix
domain socket monitor connection remained outside the TCP TLS scope.

## Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/ci-boundary-gate.sh
```

## Related Files

- `deploy/compose/docker-compose.prod.yml`
- `deploy/compose/docker-compose.standby.yml`
- `docs/current/PROJECT_MAP.md`
- `docs/current/runbooks/postgres-replication.md`
- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
