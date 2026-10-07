# Deployment Baseline

## Scope

현재 production은 중앙 호스트와 Seoul collector `b`의 Docker Compose, Osaka collector `a`와 Osaka2 collector `d`의 host-native systemd로 운영합니다. 중앙은 `deploy/compose/docker-compose.prod.yml`과 `docker-compose.live-compat.yml`을 함께 사용합니다. 관리자 웹은 iris-seoul의 `iris-console.service`가 제공합니다. 이 문서는 runtime/infra 구성의 요약 기준이며, 서비스별 적용·검증·복구 절차는 `docs/current/runbooks/`를 따릅니다.

## Non-Goals

- Docker Compose 절차 중복
- service env 전체 목록 복제

## Host Topology

| 역할 | 호스트 | 내용 |
|---|---|---|
| 중앙 런타임 (primary) | `<tailnet-central>` (`aarch64`) | Compose의 `hololive-api`, `hololive-alarm-worker`, `holo-postgres`, `valkey-cache`, shortlink ingress, autoheal, collector fleet member `youtube-collector` (`c` on 30025)와 격리 issuer `youtube-po-c`. 권위 PostgreSQL이 여기 있습니다. |
| 서울 AP·관리 웹 | `<tailnet-seoul-ap>` (`aarch64`) | Compose의 `youtube-collector-b`와 격리 issuer `youtube-po-b`, host service `iris-console.service`의 통합 관리자 웹. PostgreSQL standby나 자동 failover 대상으로 사용하지 않습니다. |
| Osaka AP `a` | `<tailnet-osaka-a>` (`x86_64`) | host-native `hololive-youtube-collector@youtube-collector-a.service`와 격리 issuer `hololive-youtube-po.service`. |
| Osaka2 AP `d` | `<tailnet-osaka2-d>` (`x86_64`) | host-native `hololive-youtube-collector@youtube-collector-d.service`와 격리 issuer `hololive-youtube-po.service`. |
| 빌드/제어 | `<build-control-host>` (`x86_64`) | 모든 컴파일·이미지 빌드·테스트. 런타임 호스트는 검증된 배포 파일과 이미지만 받습니다. |

Seoul `b`는 `docker-compose.prod.yml`과 `docker-compose.seoul.yml`을 사용합니다. Osaka `a`·Osaka2 `d`의 Compose overlay는 경로·설정 계약 검증용이며 실제 기동은 native unit이 소유합니다. 실행 방식의 설정은 `scripts/deploy/ap-hosts/{seoul,osaka,osaka2}.conf`의 `AP_RUNTIME_MODE`가 소유합니다.

`<build-control-host>`는 두 가지를 추가로 소유합니다. 첫째, CLIProxy와 observability
스택(Jaeger/OTLP, Prometheus, Loki, Grafana, exporter)이 중앙 데이터 평면 이전 때
의도적으로 남았습니다 — `CLIPROXY_BASE_URL`과 `HOLOLIVE_OTLP_GRPC_ENDPOINT`가
`<build-control-host>`를 가리키는 것은 이전 누락이 아니라 named exception입니다.
둘째, 이 호스트의 과거 Hololive DB와 자동 백업은 현재 복구 수단으로 가정하지 않습니다.
2026-09-05 로컬 `holo-postgres` container·volume과 시간별 dump/restore를 제거했고,
09-08 재도입한 일일 암호화 백업도 09-28 취소했습니다.
2026-09-29 metadata 확인에서 `hololive-db-backup.timer`는 disabled/inactive이며,
`~/.local/share/hololive-db-backup/archive/`는 비어 있고 과거 09-05 static dump는 확인되지 않았습니다.
`daily/20260927T150006Z-sql-w4-held-dsz7cbp9/`에는 delivery ledger state의 암호화 dump 1,413 bytes와
manifest/checksum만 남아 있습니다. 별도 `w4-publication-20260928/private-drop-backups/`의
Hololive ledger-state dump 4,733 bytes도 특정 객체의 복구본이지 전체 DB 백업이 아닙니다.
최신 전체 복구점은 입증되지 않았습니다. 원문 복호화·새 백업·자동화 재활성화·기존 사본 삭제는
각 대상과 손실 범위의 승인이 필요합니다. `valkey-cache`와 다른 서비스 사본은 정리 대상이 아닙니다.
`<build-control-host>`는 `x86_64`라 현재 `aarch64` primary의 물리 standby 역할을 맡지 않습니다.
이 호스트의 `hololive-compose.service`는 `disabled`로 두어 재부팅이 두 번째 alarm
dispatcher를 띄우지 못하게 합니다. 활성화는 명시적 롤백 결정을 요구합니다.
표준 `compose.sh`와 `compose-redeploy-service.sh`도 hostname이 `kapu`이면
`hololive-alarm-worker`의 직접·전체·dependency 경유 기동을 거부합니다. 승인된 롤백에서만
해당 명령에 `HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1`을 일시 지정합니다.

2026-09-08 사용자 결정에 따라 Seoul physical standby와 failover controller를 제거했으며,
Osaka `holo-postgres` 하나만 권위 primary로 운영합니다. 이 결정으로 동기화된 대기 복구와
자동 승격 역량이 사라졌습니다. Osaka primary 장애 시 새 PostgreSQL과 실제로 보존·검증한
백업이 있어야 복원할 수 있습니다. 위의 부분 객체 dump를 최신 전체 백업으로 취급하지 않습니다.

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

| Runtime | 실행 대상 | Port | Env groups | 자산 | 기동 의존성 |
|---|---|---:|---|---|---|
| `hololive-api` | 중앙 Compose `hololive-api` | 30001/30003/30006 | app file log, Iris, cache, PostgreSQL, major event, cliproxy | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey, `hololive-alarm-worker` |
| `alarm-worker` | 중앙 Compose `hololive-alarm-worker` | 30007 | app file log, Iris, cache, PostgreSQL | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey |
| `youtube-collector` | 중앙 Compose `youtube-collector` (`c`), Seoul Compose `youtube-collector-b`, Osaka native `a`, Osaka2 native `d` | `a` 30005, `b` 30015, `c` 30025, `d` 30035 | app file log, PostgreSQL (`hololive_scraper`) | 각 호스트의 logs/data, certs, worker profile, Go binary·Node helper, 격리 issuer socket | 중앙 Compose의 DB·migration 의존성과 별개로 모든 slot의 issuer를 먼저 기동·검증한 뒤 collector를 전환합니다. |

`hololive-api`는 `hololive-net`만 사용하며 Docker socket·`DOCKER_HOST`·`docker-proxy-net`에 접근하지 않습니다. 중앙 `docker-proxy`의 유일한 소비자는 `deunhealth`입니다. Collector와 issuer의 동일 source SHA 및 paired deploy/rollback 계약은 [collector runbook](runbooks/youtube-collector.md#isolated-po-token-lifecycle)을 따릅니다.

## Infra Services

| Service | Purpose | Current notes |
|---|---|---|
| `holo-postgres` | Primary PostgreSQL | Bridge-networked; live-compat publishes `<tailnet-central>:5433` explicitly to container `5432`; TLS `ssl=on`; server certificate mounted read-only from `/etc/stack-secrets/hololive-bot/postgres-tls/` |
| `holo-postgres-standby` | 재활성화 참고용 Compose 대상 | Production에서 제거했습니다. 재도입하려면 새 base backup과 명시적 승인이 필요합니다. |
| `postgres-failover.service` | 재활성화 참고용 fail-closed controller | Production unit과 timer는 제거했습니다. 저장소의 코드·unit template과 기존 원격 helper·정적 설정은 재구축 참고용으로 보존하며 자동 실행하지 않습니다. |
| `hololive-db-migrate` | Migration job | Runs before app services; uses `PGSSLMODE=verify-full` and `/run/hololive-bot/certs/postgres-ca.pem` |
| `valkey-cache` | Cache, queue, Pub/Sub | TCP and Unix socket, password required |
| `admin-dashboard-ingress` | 중앙 shortlink ingress | `docker-compose.live-compat.yml`이 정의합니다. 30192의 source-restricted shortlink와 loopback 30193의 health를 제공하며 관리자 웹을 호스팅하지 않습니다. |
| `youtube-po-c` / `youtube-po-b` | Compose의 격리 PO issuer | 중앙 `c`와 Seoul `b` collector에 private Unix socket을 제공합니다. 앱 비밀·DB·helper socket을 공유하지 않습니다. native `a`/`d`의 issuer는 각 호스트의 `hololive-youtube-po.service`입니다. |
| `docker-proxy` | Restricted Docker API proxy | internal `docker-proxy-net`에서 `deunhealth`에만 Docker API를 제공합니다. `hololive-api`와 관리자 웹은 접근하지 않습니다. |
| `deunhealth` | Autoheal sidecar | Restarts unhealthy labeled containers; old-primary fencing 동작은 비활성 HA 참고 절차에만 해당합니다. |

## External Boundaries

| Boundary | Used by | Contract doc |
|---|---|---|
| Iris / Redroid KakaoTalk automation | `hololive-api`, `alarm-worker` | `contracts/iris-boundary.md` |
| PostgreSQL | Most runtime services | schema/migration files under `hololive/hololive-api/scripts/migrations` |
| Valkey | cache, sessions/rate limit, alarm dispatch wakeup, member epoch Pub/Sub, job locks (no settings/ACL Pub/Sub) | `contracts/valkey_ephemeral_contract.md`, `QUEUE_AND_PUBSUB_CONTRACTS.md` |
| CLIPROXY/OpenAI-compatible LLM proxy | `hololive-api` where configured | 검토 필요 |

## PostgreSQL Observability

2026-10-07 Osaka primary에 `pg_stat_kcache` 2.3.2와 `pg_wait_sampling`
binary 1.1.11(SQL extension 1.1)을 활성화했습니다. 적용 PostgreSQL image revision은
`edf6f7efc2230de0667a96d5a0e3d06bccfc5004`이며, 호스트의 비밀 아닌
`HOLOLIVE_POSTGRES_PRELOAD_LIBRARIES` 설정이 기존 `pg_stat_statements`와 함께 preload합니다.
CPU·대기 통계는 기존 `pg_read_all_stats` 역할만 읽으며 앱 역할은 확장하지 않았습니다.
[적용 기록](plans/2026-10-07-postgres-extensions.md#운영-적용-결과--2026-10-07)과
[활성화·복구 절차](runbooks/postgres-observability.md)를 따릅니다.

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

The Compose client set uses `verify-full` with
`/run/hololive-bot/certs/postgres-ca.pem`: `hololive-api`, `alarm-worker`,
central `youtube-collector`, `hololive-db-migrate`, and Seoul `youtube-collector-b`.
Native Osaka APs `youtube-collector-a`/`youtube-collector-d`도 `verify-full`을 사용하며,
CA 경로는 host env generator가 지정하는 `/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem`입니다.

Operational evidence from the 2026-06-07 transition showed all 35 TCP
PostgreSQL connections on TLSv1.3 and `0` plaintext TCP connections. One Unix
domain socket monitor connection remained outside the TCP TLS scope.

## Validation

```bash
./scripts/architecture/ci-boundary-gate.sh
```

## Related Files

- `deploy/compose/docker-compose.prod.yml`
- `deploy/compose/docker-compose.live-compat.yml`
- `deploy/compose/docker-compose.seoul.yml`
- `scripts/deploy/ap-hosts/{seoul,osaka,osaka2}.conf`
- `deploy/compose/docker-compose.standby.yml`
- `docs/current/PROJECT_MAP.md`
- `docs/current/runbooks/postgres-replication.md`
