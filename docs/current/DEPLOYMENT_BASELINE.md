# Deployment Baseline

## Scope

현재 production baseline은 단일 호스트 `deploy/compose/docker-compose.prod.yml`입니다. 이 문서는 runtime/infra 구성의 요약 기준입니다. 서비스별 현재 절차는 `docs/current/runbooks/`를 따릅니다.

## Non-Goals

- k8s/k3s 재도입 설계
- Docker Compose 절차 중복
- service env 전체 목록 복제

## Host Topology

| 역할 | 호스트 | 내용 |
|---|---|---|
| 중앙 런타임 (primary) | `<tailnet-central>` (`aarch64`) | `hololive-api`, `alarm-worker`, `admin-dashboard`, `holo-postgres`, `valkey-cache`, ingress/proxy, collector fleet member `youtube-collector` (`c` on 30025). 권위 PostgreSQL이 여기 있습니다. |
| 서울 AP | `<tailnet-seoul-ap>` (`aarch64`) | `youtube-collector-b`. 기존 `holo-postgres-standby`와 failover controller는 2026-09-08 제거했으며, 복구·승격 대상으로 사용하지 않습니다. |
| 빌드/제어 | `<build-control-host>` (`x86_64`) | 모든 컴파일·이미지 빌드·테스트. 런타임 호스트는 검증된 배포 파일과 이미지만 받습니다. |
| 원격 AP | Osaka `a`, Seoul `b`, Osaka2 `d` | `a`/`d`는 host-native systemd, `b`는 Compose. |

`<build-control-host>`는 두 가지를 추가로 소유합니다. 첫째, CLIProxy와 observability
스택(Jaeger/OTLP, Prometheus, Loki, Grafana, exporter)이 중앙 데이터 평면 이전 때
의도적으로 남았습니다 — `CLIPROXY_BASE_URL`과 `HOLOLIVE_OTLP_GRPC_ENDPOINT`가
`<build-control-host>`를 가리키는 것은 이전 누락이 아니라 named exception입니다.
둘째, 같은 호스트에 남아 있던 `holo-postgres`와 dump는 과거 복구용 사본입니다. 사용자
지시로 2026-09-05 kapu의 주기적 논리 덤프와
전체 복원을 종료했으며, user `hololive-db-backup.timer`는 `disabled`·`inactive`를
유지합니다(`DEC-20260905-kapu-db-backup-retirement`). 후속 승인으로 kapu의 `holo-postgres`
container와 `hololive-bot_holo-pg-data` volume, 과거 dump 11개를 제거했습니다
(`DEC-20260905-stack-disk-cleanup`). `/home/kapu/.local/share/hololive-db-backup/`의
`hololive-20260905T004953Z.dump` 하나는 보존하지만 자동 갱신되지 않습니다. 현재 primary와
같은 데이터로 취급하지 않으며 복구에는 별도 PostgreSQL과 archive restore가 필요합니다.
`valkey-cache`는 이번 정리 대상에 포함하지 않았습니다. 자동 갱신 재개·보존 archive 삭제·복구는
각 대상과 영향에 대한 승인이 필요합니다. `<build-control-host>`는 `x86_64`라 현재
`aarch64` primary의 물리 standby 역할을 맡지 않습니다.
이 호스트의 `hololive-compose.service`는 `disabled`로 두어 재부팅이 두 번째 alarm
dispatcher를 띄우지 못하게 합니다. 활성화는 명시적 롤백 결정을 요구합니다.
표준 `compose.sh`와 `compose-redeploy-service.sh`도 hostname이 `kapu`이면
`hololive-alarm-worker`의 직접·전체·dependency 경유 기동을 거부합니다. 승인된 롤백에서만
해당 명령에 `HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1`을 일시 지정합니다.

2026-09-08 사용자 결정에 따라 Seoul physical standby와 failover controller를 제거했으며,
Osaka `holo-postgres` 하나만 권위 primary로 운영합니다. 이 결정으로 동기화된 대기 복구와
자동 승격 역량이 사라졌으며, Osaka primary 장애 시 새 PostgreSQL을 준비하고 보존 백업을
복원해야 합니다.
2026-09-05 보존한 static dump는 자동 갱신되지 않아 최신 백업이 아닙니다.

API, alarm worker와 중앙 collector `c`는 같은 Docker network의 `holo-postgres:5432`에
직접 연결하고, 원격 collector `a/b/d`는 Osaka Tailscale IP `100.100.1.8:5433`에 직접
연결합니다. 여섯 consumer 모두 TLS `verify-full`을 유지하며 DB용 Tailscale Service 중계를
사용하지 않습니다. Seoul의 standby container·전용 PGDATA volume, failover
timer·service·apply drop-in과 Osaka의 `iris_seoul_standby` physical slot을 제거했습니다.
Osaka와 Seoul에는 `svc:hololive-postgres` serve route가 없습니다. primary는 재시작하지
않았으며 DB 접속 설정을 반영한 consumer 여섯 개만 순차 재기동했습니다.

`deploy/compose/docker-compose.standby.yml`과 failover 구현은 재활성화 참고 자료로만
유지합니다. 다시 사용하려면 대상·데이터 비용·fencing·route 영향에 대한 명시적 승인을 새로
받고, 당시 Osaka primary에서 새 `pg_basebackup`을 생성해 Seoul을 재시딩한 뒤
`runbooks/postgres-replication.md`의 전체 검증을 통과해야 합니다. 삭제한 PGDATA나 예전
timeline을 그대로 재기동하지 않습니다.

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
| `youtube-collector` | `youtube-collector` | 30025 (`c`; AP `a/b/d` 30005/30015/30035) | app file log, PostgreSQL (`hololive_scraper`) | `data`, `logs` | PostgreSQL, migration |

## Infra Services

| Service | Purpose | Current notes |
|---|---|---|
| `holo-postgres` | Primary PostgreSQL | Bridge-networked; live-compat publishes `<tailnet-central>:5433` explicitly to container `5432`; TLS `ssl=on`; server certificate mounted read-only from `/etc/stack-secrets/hololive-bot/postgres-tls/` |
| `holo-postgres-standby` | 재활성화 참고용 Compose 대상 | Production에서 제거했습니다. 재도입하려면 새 base backup과 명시적 승인이 필요합니다. |
| `postgres-failover.service` | 재활성화 참고용 fail-closed controller | Production unit과 timer는 제거했습니다. 저장소의 코드·unit template과 기존 원격 helper·정적 설정은 재구축 참고용으로 보존하며 자동 실행하지 않습니다. |
| `hololive-db-migrate` | Migration job | Runs before app services; uses `PGSSLMODE=verify-full` and `/run/hololive-bot/certs/postgres-ca.pem` |
| `valkey-cache` | Cache, queue, Pub/Sub | TCP and Unix socket, password required |
| `admin-dashboard` | Dashboard frontend | Port 30190, not part of Go runtime count |
| `docker-proxy` | Restricted Docker API proxy | Used instead of mounting the Docker socket directly |
| `deunhealth` | Autoheal sidecar | Restarts unhealthy labeled containers; old-primary fencing 동작은 비활성 HA 참고 절차에만 해당합니다. |

## External Boundaries

| Boundary | Used by | Contract doc |
|---|---|---|
| Iris / Redroid KakaoTalk automation | `hololive-api`, `alarm-worker` | `contracts/iris-boundary.md` |
| PostgreSQL | Most runtime services | schema/migration files under `hololive/hololive-api/scripts/migrations` |
| Valkey | cache, alarm queue, config Pub/Sub | `QUEUE_AND_PUBSUB_CONTRACTS.md` |
| CLIPROXY/OpenAI-compatible LLM proxy | `hololive-api` where configured | 검토 필요 |

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
central `youtube-collector`, `hololive-db-migrate`, Seoul `youtube-collector-b`,
and Osaka APs `youtube-collector-a`/`youtube-collector-d` when they are rolled out.

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
