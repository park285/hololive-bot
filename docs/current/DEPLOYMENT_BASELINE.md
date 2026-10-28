# Deployment Baseline

## Scope

현재 production은 중앙 호스트와 Seoul collector `b`의 Docker Compose, Osaka collector `a`와 Osaka2 collector `d`의 host-native systemd로 운영합니다. 중앙은 `deploy/compose/docker-compose.prod.yml`과 `docker-compose.live-compat.yml`을 함께 사용합니다. 관리자 웹은 iris-seoul의 `iris-console.service`가 제공합니다. 이 문서는 runtime/infra 구성의 요약 기준이며, 서비스별 적용·검증·복구 절차는 `docs/current/runbooks/`를 따릅니다.

### Prepared 11.0.0 worker/DB cutover — not activated

After actual old-profile-compatible 10.2.0 acceptance, the 11.0.0 source prepares
immutable worker profile pairs, the standalone `docker-compose.collector.yml` AP
base, private Unix-socket migration, role-based TCP rejection, keepalive/logging/WAL
command settings, and revised pool budgets.
It does **not** establish that production has accepted those changes. Preserve the
deployment facts below until an approved rollout records fresh image/profile,
render, SQL transport/role, capacity and readiness evidence.

Accept the pairing baseline before incompatible profile settings; the final binary
has no legacy settings shim. Preserve the full upstream migration manifest through
324 and already-applied stream-time work; this cutover adds no data migration.
Master/manifest and password-file preparation, role/owner/HBA changes, server
recreation, `postgresql.auto.conf` cleanup, retired-role deletion, and noeviction
activation are separate approved operations. Only the rollout owner activates role
limits after all four collectors are accepted at max 7. Keep actual source/runtime
state distinct from the target configuration documented in the repository.

## Non-Goals

- Docker Compose 절차 중복
- service env 전체 목록 복제

## Host Topology

| 역할 | 호스트 | 내용 |
|---|---|---|
| 서울 중앙 런타임 (primary) | `<tailnet-central>` (`aarch64`) | 서울의 별도 A1 2 OCPU·12GB·50GB VM. Compose의 `hololive-api`, `hololive-alarm-worker`, `holo-postgres`, `valkey-cache`, shortlink ingress, autoheal, collector fleet member `youtube-collector` (`c` on 30025)와 격리 issuer `youtube-po-c`. 권위 PostgreSQL이 여기 있습니다. |
| 서울 AP·관리 웹 | `<tailnet-seoul-ap>` (`aarch64`) | Compose의 `youtube-collector-b`와 격리 issuer `youtube-po-b`, host service `iris-console.service`의 통합 관리자 웹. PostgreSQL standby나 자동 failover 대상으로 사용하지 않습니다. |
| Osaka AP `a` | `<tailnet-osaka-a>` (`x86_64`) | host-native `hololive-youtube-collector@youtube-collector-a.service`와 격리 issuer `hololive-youtube-po.service`. |
| Osaka2 AP `d` | `<tailnet-osaka2-d>` (`x86_64`) | host-native `hololive-youtube-collector@youtube-collector-d.service`와 격리 issuer `hololive-youtube-po.service`. |
| 빌드/제어 | `<build-control-host>` (`x86_64`) | 모든 컴파일·이미지 빌드·테스트. 런타임 호스트는 검증된 배포 파일과 이미지만 받습니다. |

Seoul `b`는 `docker-compose.prod.yml`과 `docker-compose.seoul.yml`을 사용합니다. Osaka `a`·Osaka2 `d`는 Compose overlay 없이 native systemd unit 하나로만 기동합니다. 실행 방식의 설정은 `scripts/deploy/ap-hosts/{seoul,osaka,osaka2}.conf`의 `AP_RUNTIME_MODE`가 소유합니다.

2026-10-07 중앙 runtime과 PostgreSQL을 Osaka에서 Seoul로 이전했습니다. 새 VM이 기존 중앙
주소 `100.100.1.8`을 승계했으므로 AP·Iris webhook·관리 gateway·DB exporter·shortlink의
주소 계약은 그대로입니다. AP `a/b/d`는 기존 호스트에 남고, 새 중앙 `c`만 이전했습니다.
동일 ARM64 운영 이미지와 전체 PostgreSQL cold copy를 사용했으며 추가 schema migration은 없었습니다.
이관 검증 후 사용자 승인으로 옛 중앙 VM을 삭제하여 `TERMINATED`를 확인했습니다.
100GB 부트 볼륨은 분리된 `AVAILABLE` 상태로 디스크·이미지·배포 파일을 보존합니다.
복구에는 새 VM 생성이 필요하며, 옛 관리 주소 `100.100.1.9`는 현재 접속 가능한 서버가 아닙니다.
보존 볼륨의 부팅 unit은 disabled이고, 종료 전에 container restart policy를 `no`로 설정했습니다.
`fence.intent`가 자동 primary 재기동을 막습니다. 서울 쓰기 재개 이후에는 최신 데이터를
역이관하지 않고 옛 DB를 다시 primary로 켜면 안 됩니다.

2026-10-08 서울 중앙을 같은 A1 2 OCPU·12GB·50GB 규격의 새 Ubuntu 26.04.1 LTS VM으로
교체했습니다. 격리된 DB·앱 시험과 재부팅 검증 후 기존 `hololive-seoul` 이름과 `.8`을
승계했습니다. 운영 이미지 10개를 그대로 사용하고, clean-stop한 PostgreSQL 전체 파일의
내용·권한 해시 일치 및 페이지 체크섬 오류 0개를 확인한 뒤 쓰기를 재개했습니다.
schema migration은 실행하지 않았습니다. 새 호스트의 `hololive-compose-up`은 부팅 때
승인된 runtime 서비스만 `--no-build --no-deps`로 기동하며 migration one-shot은 제외합니다.
운영 재부팅 후 중앙·AP `a/b/c/d` readiness, 관리 gateway, 실제 DB TLS, metrics·로그 전달을 확인했습니다.
옛 24.04 서울 인스턴스는 `TERMINATED`입니다. 초기 보존했던 50GB 부트 볼륨도 같은 날
사용자의 추가 삭제 승인으로 `TERMINATED`를 확인했습니다. 이 서울 사본에서는 더 이상
복구할 수 없습니다. Osaka 보존 디스크와 기존 별도 백업은 변경하지 않았으며 정기 백업은 재개하지 않았습니다.
새 호스트의 Docker socket GID는 `987`이며 secret master와 운영 mirror의 비밀 아닌 값도 일치합니다.

`<build-control-host>`는 두 가지를 추가로 소유합니다. 첫째, CLIProxy와 observability
스택(Jaeger/OTLP, Prometheus, Loki, Grafana, exporter)이 중앙 데이터 평면 이전 때
의도적으로 남았습니다 — `CLIPROXY_BASE_URL`과 `HOLOLIVE_OTLP_GRPC_ENDPOINT`가
`<build-control-host>`를 가리키는 것은 이전 누락이 아니라 named exception입니다.
둘째, 원격 primary의 일일 암호화 논리 백업과 주간 격리 복원을 소유합니다.
과거 로컬 `holo-postgres` container·volume과 시간별 dump/restore는 복구하지 않습니다.
2026-10-07 전체 DB 복원·Drive 전송 검증 이후 사용자 요청으로 자동·수동 백업과 정기 복원
검증을 중단했습니다. 관련 timer는 disabled/inactive이며, 이번 중앙 이전에서도 재개하지
않았습니다. 기존 백업과 개별 ledger-state dump는 보존하되 전체 DB 복구본과 구분합니다.
전용 백업 SSH 경로의 새 primary 반영·재검증은 백업 재개 승인 때 수행합니다.
독립 복구키 보관·검증은 사용자 요청으로 제외했으므로 build-control host 전체 유실 시의
키 가용성은 보장하지 않습니다.
`valkey-cache`와 다른 서비스 사본은 정리 대상이 아닙니다.
`<build-control-host>`는 `x86_64`라 현재 `aarch64` primary의 물리 standby 역할을 맡지 않습니다.
이 호스트의 `hololive-compose.service`는 `disabled`로 두어 재부팅이 두 번째 alarm
dispatcher를 띄우지 못하게 합니다. 활성화는 명시적 롤백 결정을 요구합니다.
표준 `compose.sh`와 `compose-redeploy-service.sh`도 hostname이 `kapu`이면
`hololive-alarm-worker`의 직접·전체·dependency 경유 기동을 거부합니다. 승인된 롤백에서만
해당 명령에 `HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1`을 일시 지정합니다.

2026-09-08 사용자 결정으로 Seoul physical standby와 failover controller를 제거했습니다.
2026-10-07 이전 이후에는 새 Seoul 중앙 `holo-postgres` 하나만 권위 primary입니다.
동기화된 대기 복구나 자동 승격은 없으며, 보존한 Osaka DB는 전환 시점의 정지 사본입니다.
부분 객체 dump를 전체 백업으로 대체하거나, 오래된 사본을 현재 primary로 오인하지 않습니다.

API, alarm worker와 중앙 collector `c`는 같은 Docker network의 `holo-postgres:5432`에
직접 연결하고, 원격 collector `a/b/d`는 Seoul 중앙 Tailscale IP `100.100.1.8:5433`에 직접
연결합니다. 여섯 consumer 모두 TLS `verify-full`을 유지하며 DB용 Tailscale Service 중계를
사용하지 않습니다. Seoul의 standby container·전용 PGDATA volume, failover
timer·service·apply drop-in과 Osaka의 `iris_seoul_standby` physical slot을 제거했습니다.
당시 제거한 `svc:hololive-postgres` serve route는 이번 이전에서도 재생성하지 않았습니다.
주소를 승계한 새 primary에 consumer들이 직접 재접속하며 TLS `verify-full`을 유지합니다.

`deploy/compose/docker-compose.standby.yml`과 failover 구현은 재활성화 참고 자료로만
유지합니다. 다시 사용하려면 대상·데이터 비용·fencing·route 영향에 대한 명시적 승인을 새로
받고, 당시의 현재 primary에서 새 `pg_basebackup`을 생성해 standby를 재시딩한 뒤
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
binary 1.1.11(SQL extension 1.1)을 활성화한 뒤, 같은 날 서울 중앙으로 이전하면서
설정·확장·전용 스키마·권한을 보존했습니다. 2026-10-27(UTC)에는 같은 절차로
`holo-postgres`만 재생성하여 `auto_explain`을 preload에 더하고 `pg_buffercache` 1.6과
`amcheck` 1.5를 같은 스키마에 활성화했습니다. 현재 `hololive-seoul`의 PostgreSQL image revision은
`12588674f2ddfac1a784c145b0a66620800691cf`(image ID `sha256:5606cad5c443…`)이며, 호스트의 비밀 아닌
`HOLOLIVE_POSTGRES_PRELOAD_LIBRARIES` 설정이 `pg_stat_statements,pg_stat_kcache,pg_wait_sampling,auto_explain`을
preload합니다. 직전 image `hololive-postgres:infra-58fc86436-20261026T044001Z-arm64`와 이전 compose 파일은
`/opt/hololive-bot/compose/deploy-backups/pre-pg-auto-explain-20261027T150624Z/`에 복구점으로 남겼습니다.
새 통계는 `hololive_observability` 스키마에서 기존 `pg_read_all_stats` 역할이 읽습니다.
exporter의 public USAGE 차단과 앱 역할의 권한은 유지했습니다.
[적용 기록](plans/2026-10-07-postgres-extensions.md#운영-적용-결과--2026-10-07)과
[활성화·복구 절차](runbooks/postgres-observability.md)를 따릅니다.
이후 별도 계측 브랜치의 검증된 소스를 로컬 main에 통합했습니다. 운영 이미지·실행 설정은
그대로이며, 소스 통합을 이유로 DB를 재생성하거나 확장 활성화 SQL을 다시 실행하지 않습니다.

## PostgreSQL Resource Baseline

2026-10-14 중앙 `holo-postgres`의 cgroup 메모리 한도를 2GiB에서 4GiB로,
`max_wal_size`를 1GB에서 2GB로 무중단 조정했습니다. 이어 사용자 승인 아래 공유 버퍼와
WAL 버퍼, 캐시 추정치를 아래 값으로 조정하고 같은 ARM64 이미지로 DB 컨테이너만 재생성했습니다.
운영 명령은 `--no-build --no-deps --pull never --timeout 120 --wait`를 사용했으며
재생성부터 healthy까지 약 10초였습니다. schema migration과 다른 서비스 재배포는 없었습니다.

| 설정 | 현재 값 | 영속 설정 소유 경로 |
|---|---|---|
| 컨테이너 메모리 한도 | 4GiB | `deploy/compose/docker-compose.prod.yml`의 `holo-postgres.deploy.resources.limits.memory`; 운영 deploy tree도 동일 |
| `shared_buffers` | 1GB | Compose 시작 인자; 컨테이너 한도의 25%를 배정하고 프로세스·파일 캐시 여유 유지 |
| `wal_buffers` | 64MB | Compose 시작 인자; 피크의 WAL 버퍼 포화에 대비 |
| `effective_cache_size` | 3GB | Compose 시작 인자; 공유 버퍼와 파일 캐시를 고려한 planner 추정치이며 별도 메모리 할당 아님 |
| `work_mem` / `maintenance_work_mem` | 4MB / 128MB | 기존 Compose 시작 인자 유지; 동시 쿼리·autovacuum의 메모리 배수를 고려 |
| `max_wal_size` | 2GB | 기존 `holo-pg-data`의 `pgdata/postgresql.auto.conf`; `ALTER SYSTEM`과 설정 reload로 적용 |
| `checkpoint_timeout` / `checkpoint_completion_target` | 15min / 0.9 | 기존 `postgresql.auto.conf` / PostgreSQL 기본값 유지 |
| `wal_compression` | `lz4` | 기존 `postgresql.auto.conf` 유지 |

메모리 한도에는 프로세스·공유 버퍼뿐 아니라 파일 캐시도 계상됩니다. `max_connections=61`(postgres_exporter의 collector 연결 1개를 예산에 더한 2026-10-25 값),
`fsync=on`, `synchronous_commit=on`, `full_page_writes=on`과 OCI 50GB·10 VPU/GB를 유지합니다.
`max_wal_size`는 WAL 공간의 절대 상한이 아니며, 상향하면 보관 공간과 crash recovery 시간이
늘어날 수 있습니다. 신규 PGDATA 생성 시 기존 `postgresql.auto.conf`는 승계되지 않으므로
승인된 초기 구성에서 위 운영 값을 별도로 적용해야 합니다.

재기동 로그의 clean shutdown·ready, 실제 API·worker·collector `a/b/c/d` health/readiness와
원격 AP의 DB TLSv1.3 재연결을 확인했습니다. DB 시작 시각은 `2026-10-14T12:33:12Z`이며
이미지 revision `af9b2b57cbf2e18b34f9ccecc46b28396e054ba3`는 유지했습니다.
재기동 후 확인한 조정값과 `max_connections`의 `pending_restart`는 모두 false였습니다.
파일 기본값과 Compose 시작 인자가 다른 항목은 이후 reload에서 다시 재시작 필요 표시가
나타날 수 있으므로 실제 값과 `source=command line`을 함께 확인합니다.

재기동 뒤 239초 구간에서 DB commit 16,636건이 진행됐고 WAL 버퍼 포화·cgroup 메모리 상한
도달·OOM 증가량은 모두 0이었습니다. 별도 120초 디스크 관측의 평균 CPU I/O 대기는 0.35%였습니다.
재기동 구간에는 종료·기동 중 DB 접속 오류가 일시적으로 발생했습니다. healthy 이후 30초부터
확인한 중앙 DB·API·worker·collector와 AP a/b/d 로그에서는 추가 오류를 관측하지 않았습니다.

버퍼 조정 직전의 Compose는 중앙
`/var/backups/hololive-postgres-buffers-20261014T123219Z/docker-compose.prod.yml`에 보존합니다.
이 단계의 원복은 이후 다른 변경을 덮어쓰지 않도록 공유 버퍼 512MB·캐시 추정치 1536MB와
기본 WAL 버퍼 16MB 구성을 저장소·운영 Compose에 함께 복구한 뒤 같은 이미지로 DB만 재생성합니다.
4GiB 메모리 제한과 2GB WAL 한도는 유지합니다.

첫 무중단 조정 전 Compose와 auto-conf는
`/var/backups/hololive-postgres-tuning-20261014T121804Z/`에 별도로 보존합니다.
그 단계까지 원복하려면 승인된 범위에서 `ALTER SYSTEM RESET max_wal_size`와 reload를 사용하고,
메모리 회수 영향을 확인한 뒤 실행 한도와 Compose를 함께 2GiB로 되돌립니다.
전체 auto-conf 복원으로 이후의 다른 설정 변경을 덮어쓰지 않습니다.
피크 개선은 다음 유사 부하에서 WAL 기반 체크포인트 횟수·버퍼 포화·쓰기 대기시간·서비스 지연으로
판단하며, 재시작 직후의 캐시 예열이나 평상시 health 통과를 피크 처리량 향상의 증거로 대신하지 않습니다.

## PostgreSQL TLS Baseline

Production PostgreSQL access is certificate-verified end to end. `holo-postgres`
loads a server certificate issued by the `iris-stack internal CA` covering
`holo-postgres`, `host.docker.internal`, `localhost`, the central host name and
its tailnet FQDN, the central tailnet/private/public IPs, and `127.0.0.1`/`::1`.
The certificate is a long-lived static file (the Seoul leaves are valid through 2031-10), not an
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
