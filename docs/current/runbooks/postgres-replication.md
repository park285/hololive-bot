# Runbook: PostgreSQL single-primary baseline and replication reactivation

## Role

현재 운영 결정은 중앙 primary(`hololive-seoul` / `100.100.1.8`) 하나만 사용하는
single-primary 구성입니다. Seoul AP 호스트(`iris-seoul` / `100.100.1.5`)의
`holo-postgres-standby`, 복제 슬롯과 failover controller는 2026-09-08 사용자 결정에 따라
제거했습니다.

아래 safety model·Dependencies·재활성화 전제는 물리 복제와 failover를 다시 들일 때의 검토
기준입니다. 대기 서버 unit(`deploy/quadlet/hololive-bot-standby/`), failover controller·fence·route
코드와 그 bootstrap·설치·smoke·비상 실행 절차는 저장소에서 지웠고 커밋 `bb000290e`에 남아 있습니다.
다시 사용하려면 명시적 승인을 새로 받고 대상 호스트용 정의를 새로 승인받은 뒤, 당시 primary에서 새
`pg_basebackup`으로 standby를 재시딩해야 합니다.

`kapu`의 기존 시간별 dump/restore는 사용자 지시로 2026-09-05 종료했습니다.
후속 승인으로 로컬 `holo-postgres` container와
`hololive-bot_holo-pg-data` volume을 제거했습니다.
2026-09-29 metadata 확인에서는 과거 09-05 static dump가 없고 `archive/`도 비어 있습니다.
`kapu`는 `x86_64`이므로 현재 `aarch64` primary의 물리 standby로 전환하지 않습니다.
Seoul 복제를 제거하여 동기화된 대기 복구와 자동 승격 역량도 없습니다.
09-08 재도입한 일일 암호화 백업은 09-28 취소되었으며 `hololive-db-backup.timer`는
disabled/inactive입니다. 남은 09-27 암호화 dump와 별도 09-28 dump는 delivery ledger state
한 객체의 사본이지 전체 DB 백업이 아닙니다. 최신 전체 복구본은 입증되지 않았습니다.
정확한 metadata와 경계는 `../DEPLOYMENT_BASELINE.md`를 따릅니다.
복구 시점은 실제 검증한 백업 세대의 생성 시각에 따릅니다. 새 복구본·자동 백업 재활성화,
Seoul 복제 재구축이나 보존 자료 삭제는 각 대상과 영향에 대한 명시적 승인이 필요합니다.

## Logical restore check: database-level settings

논리 복원으로 클러스터나 `hololive` DB를 새로 만들 때 `ALTER DATABASE ... SET` 기본값은 평문
`pg_dump --create`·`pg_dumpall` 출력과 archive 형식의 `pg_restore --create`에는 포함되지만, `--create` 없는
`pg_restore`(예: `--clean --if-exists`)로는 빠집니다. `schema_migrations`는 데이터로
복원되므로 러너는 그 값을 만든 migration을 다시 실행하지 않습니다. 2026-10-08 운영에는 183의
`idle_in_transaction_session_timeout=5min`이 없었습니다. 이를 다시 선언하는 migration 271을 2026-10-07 20:11 UTC에
적용했고, 아래 2단계의 새 세션 확인에서 `5min`을 확인했습니다. 2026-08-23 PG 18.6 전환의
`pg_restore` 플래그 기록이 없어 그 복원이 원인이라는 판단은 추론입니다. 물리 복사(cold copy·basebackup)는 이 설정을
그대로 옮깁니다.

1. archive는 `pg_restore --create`로 복원합니다. `--create` 없이 복원했다면 승인된 복원 작업 안에서 DB
   owner(`hololive_migrator`)로 `ALTER DATABASE hololive SET idle_in_transaction_session_timeout = '5min';`을 다시
   실행합니다.
2. 애플리케이션 role로 **새 세션**을 열어 확인합니다. `SHOW idle_in_transaction_session_timeout`이 `5min`이고
   `SELECT source FROM pg_settings WHERE name = 'idle_in_transaction_session_timeout'`가 `database`여야 합니다.
   기존 pool connection은 이전 값을 유지하므로 확인 뒤 애플리케이션 pool을 순차 재기동합니다.

## Current single-primary decision

The current authority is `hololive-seoul` / `100.100.1.8`, owned by
`deploy/topology/hosts.conf`. Only its `holo-postgres` is a read/write primary.
The old Osaka disk is not a synchronized standby or an automatic recovery target.
The removed iris-seoul standby/container/slot/controller must not be inferred from
the retained reference files. Any reactivation needs fresh approval, a new base backup,
explicit replication TLS/HBA routing, slot capacity/alerts and fencing acceptance.

API, alarm worker and collector `c` use `holo-postgres:5432`; remote collectors `a/b/d`
use the current Seoul primary `100.100.1.8:5433`, all with TLS `verify-full`.
The prepared 11.0.0 migrator/bootstrap uses the central-only shared Unix socket,
with explicit local `sslmode=disable`; its HBA denies admin/migrator TCP before
network allows. This target is not live acceptance evidence. Operator and nightly
`podman exec` sockets remain supported; never mount that socket in runtime/AP
containers. The recorded active primary has no physical replication slots.

The central credential/config master owner is `hosts/hololive-seoul/hololive-bot/compose.env`.
The iris-seoul collector b unit names `100.100.1.8:5433` (`deploy/podman/iris-seoul/`); native host.env is generated from
the non-secret topology owner, with pool maximum from the capacity policy. Collector b reads only its scoped
`collector.env`, not the central production file; verify the actual deployed layout at cutover.
The workstation `.3` is not a PostgreSQL fallback. A route rollback needs explicit
authorization and all affected consumers/certificates; binary rollback alone is not
authorization to reinstate retired DNS routes.

Prepared WAL settings are command-line `checkpoint_timeout=1800s`, `max_wal_size=2GB`,
`wal_compression=lz4` in production; a reactivated standby definition must carry the same values. No source edit cleans
existing `postgresql.auto.conf`; that exact cleanup, role-limit changes, HBA installation,
DB recreation and refreshed-session acceptance remain separate approved operations.
Capture old values/sources and preserve matching WAL flags when rolling back old unit definitions.

## Inactive HA reference: safety model

이 절과 Dependencies의 failover 설계는 production에서 비활성인 재활성화 참고 기준이며, 이를 구현한
controller는 커밋 `bb000290e`에 남아 있습니다.
명시적 재승인, 새 base backup, fencing/route 재검증 없이 실행하지 않습니다. 아래 Stable
database endpoint도 HA 재활성화 때 검토하는 참고 경로이며 현재 consumer는 사용하지 않습니다.

자동 승격은 단순 연결 실패만으로 실행되지 않습니다. 다음 조건을 모두 만족해야 합니다.

1. primary read/write probe가 기본 4회 연속 실패하고 최초 실패 뒤 45초가 지납니다.
2. 마지막 정상 관측이 120초 이내이고, 그 관측에서 primary LSN과 standby replay LSN의
   알려진 지연이 기본 `0 byte`였습니다.
3. 현재 standby가 recovery/read-only 상태이고 WAL replay가 일시정지되지 않았으며,
   마지막으로 관측한 primary LSN 이상을 replay했습니다.
4. 전용 `hololive-pg-failover` 사용자가 root-owned fencing hook을 실행하고, 구 primary의
   제한된 root helper가 writer 재등장을 차단한 뒤
   `FENCED|<primary-host>|<new-primary-host>:<new-primary-port>|<request-id>|<fence-token>`을 반환합니다.
5. fencing 직후 구 primary를 다시 probe했을 때 read/write primary로 응답하지 않습니다.
6. 승격 후 별도 권한 경계의 route hook이 권위 DB endpoint를 새 primary로 전환하고
   `ROUTED|<new-primary-host>:<new-primary-port>|<fence-token>`을 반환합니다.

커밋 `bb000290e`의 controller systemd unit은 기본이 `--dry-run`이었습니다. apply drop-in이
없으면 fencing, `pg_promote()`, route 변경을 실행하지 않았고, apply 모드도 fencing hook과
route hook 중 하나라도 없거나 안전 검증에 실패하면 승격 전에 중단했습니다.

이 구조는 비동기 복제의 RPO를 0으로 만들지 않습니다. 마지막 정상 샘플 이후 primary에
commit됐지만 standby로 전송되기 전에 primary가 사라진 트랜잭션은 알 수 없습니다.
`MAX_KNOWN_LAG_BYTES=0`은 알려진 손실을 거부하는 방어선이지 synchronous commit 보장이
아닙니다.

## Dependencies

| 대상 | 내용 |
|---|---|
| primary `pg_hba.conf` | `deploy/postgres/pg_hba.conf` has no active replication TCP allow rule. Reactivation requires a separately approved `hostssl replication hololive_replicator <standby-ip>/32` rule before the replication reject, with the actual transport/source validated. The retired `.5` rule is not retained as an inactive allow. `host all all all` does not match the physical `replication` pseudo-database. |
| primary 복제 역할 | `hololive_replicator`. init-db가 `HOLOLIVE_REPLICATOR_PASSWORD`가 있을 때만 만듭니다. |
| failover DB 권한 | `hololive_replicator`에 `hololive` CONNECT와 `pg_catalog.pg_promote(boolean, integer)` EXECUTE만 부여합니다. OS controller에 admin/superuser 자격을 주지 않습니다. |
| primary 복제 슬롯 | `iris_seoul_standby` physical slot. standby가 오래 끊기면 슬롯이 WAL을 보존해 primary 디스크가 찰 수 있습니다. 재활성 전에 아래 [slot WAL 보존 상한](#reactivation-prerequisite-slot-wal-retention-bound)을 먼저 갖춥니다. |
| standby 자격 | `/etc/stack-secrets/hololive-bot/postgres/pgpass` (`0600 999:999`). PostgreSQL 컨테이너는 스택 공통 uid 999로 실행되므로 그 uid가 읽을 수 있어야 합니다. |
| standby CA | `/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem`. `primary_conninfo`와 controller probe가 `sslmode=verify-full`을 사용합니다. |
| host client | PGDG `postgresql-client-18`의 canonical `/usr/lib/postgresql/18/bin/psql`. `/usr/bin/psql`의 `pg_wrapper` symlink는 trusted-path 검사에서 거부합니다. controller는 Docker socket 또는 `docker` 그룹을 사용하지 않습니다. |
| failover env | `/etc/stack-secrets/hololive-bot/postgres-failover.env` (`0600 root:root`). 커밋 `bb000290e`의 controller unit은 systemd `LoadCredential`로 전용 사용자에게 read-only 사본을 전달했고 allowlist launcher가 해석했습니다. |
| fencing backend | 구 primary가 절대로 writer로 재등장하지 않게 하는 외부 증명입니다. SSH reference hook은 호스트가 reachable할 때만 유효합니다. 전원/호스트 상실까지 자동 처리하려면 hypervisor/cloud/PDU 같은 out-of-band fence hook이 필요합니다. |
| route backend | Tailscale Service `svc:hololive-postgres`, client endpoint `hololive-postgres.tail742dd8.ts.net:5433`. old primary drain 뒤 새 primary의 tailnet IP/port를 광고하고 stable endpoint가 read/write인지 검증합니다. |

## Reactivation prerequisite: slot WAL retention bound

현재 slot은 0개라 이 위험은 없습니다. HA 재활성을 다시 승인할 때는 아래 세 가지를 재활성 변경에
함께 넣고, 갖추지 못하면 slot을 만들지 않습니다. 지금 unit에는 이 상한을 넣지 않습니다. 적용은
재활성 변경과 함께 hololive-bot-ops가 수행합니다.

1. **WAL 보존 상한:** `holo-postgres.container`와 새로 승인한 standby unit의 PostgreSQL
   `Exec=` 모두에 `-c max_slot_wal_keep_size=<값>`을 둡니다. 승격하면 standby unit이 primary가
   되므로 한쪽에만 두지 않습니다. 설정이 없으면 PostgreSQL 기본값 `-1`(무제한)이 적용되어 standby가
   끊긴 동안 slot이 WAL을 계속 보존합니다. 값은 재활성 시점의 primary 여유 디스크와 WAL 생성 속도를
   실측해 다시 산정합니다(예: 여유의 약 1/3, 15GB 안팎). 상한이 없으면 단절이 며칠만 이어져도
   primary 디스크가 찰 수 있습니다(2026-09 기록 기준 추정 약 3–5일, Osaka 여유 46GiB는 09-26 실측).
   `Exec=`의 `-c` 값은 postmaster command line이므로 바꾸려면 `holo-postgres.service`를 재시작해야 합니다.
2. **slot 생성 단계:** 2026-09-08에 slot을 제거했으므로 `pg_basebackup -S`만으로는 slot이 없어
   실패합니다. `-C`(`--create-slot`)를 함께 써서 base backup과 같은 실행에서
   slot을 만듭니다. 같은 이름의 slot이 이미 있으면 `-C`가 실패하므로, 남은 slot을 확인한 뒤
   진행합니다.
3. **slot 지표 경보:** postgres_exporter의 replication slot 지표(`wal_status`, `safe_wal_size`)로
   Prometheus alert를 둡니다. 지표 이름과 collector 활성 여부는 당시 exporter 버전에서 확인합니다.
   경보 규칙은 stack-platform-ops observability가 소유합니다. nightly 수집은 감시 수단으로 쓰지
   않습니다.

상한을 넘으면 slot이 invalidated(`wal_status=lost`)되고 standby는 더는 따라잡을 수 없습니다. 이때는
새 `pg_basebackup`으로 재시딩합니다. primary 디스크를 지키려고 의도한 trade-off입니다. PG18의
`idle_replication_slot_timeout`은 시간 기준 보조안으로만 검토하고, 1차 방어는 크기 상한으로 둡니다.

## Reactivation reference: Stable database endpoint

HA를 재활성화할 때는 모든 중앙·AP consumer를 다음 service endpoint로 전환하는 방식을
검토합니다. 현재 single-primary의 직접 연결 설정을 그대로 덮어쓰지 않습니다.

```text
HOLOLIVE_CENTRAL_POSTGRES_HOST=hololive-postgres.tail742dd8.ts.net
HOLOLIVE_CENTRAL_POSTGRES_PORT=5433
```

현재 두 DB host의 service route는 제거된 상태입니다. 재활성화할 때는 Tailscale admin
console의 Service 정의·승인 상태와 `tcp:5433` endpoint를 다시 확인하고, 광고하는 primary
target과 service DNS의 read/write probe가 일치하는지 검증합니다.

Tailscale Service host는 tagged device여야 합니다. tailnet policy에는 service host 전용
`tag:hololive-db`, service auto-approver, 그리고 PostgreSQL consumer에서 TCP 5433으로 가는
grant를 먼저 반영합니다. 기존 tag를 제거하지 않습니다.

```json
{
  "tagOwners": {
    "tag:hololive-db": ["autogroup:admin"]
  },
  "autoApprovers": {
    "services": {
      "svc:hololive-postgres": ["tag:hololive-db"]
    }
  },
  "grants": [
    {
      "src": ["tag:vm", "tag:hololive-db"],
      "dst": ["svc:hololive-postgres"],
      "ip": ["tcp:5433"]
    }
  ]
}
```

정책 전체를 위 조각으로 덮어쓰지 않습니다. 현재 tailnet policy에 병합하고 admin console의
policy test를 통과시킨 뒤 적용합니다. 아래 설정은 복제와 service 중계를 명시적으로
재활성화할 때만 적용하는 참고 절차입니다.

```bash
# 재활성화 참고: standby는 구성 직후 drain해 read-only endpoint를 노출하지 않습니다.
sudo tailscale serve --yes --service=svc:hololive-postgres --tcp=5433 tcp://100.100.1.5:5434
sudo tailscale serve drain svc:hololive-postgres

# 재활성화 참고: primary만 service를 광고합니다.
sudo tailscale serve --yes --service=svc:hololive-postgres --tcp=5433 tcp://100.100.1.8:5433
```

재활성화 후 `tailscale serve get-config --all`에서 Osaka target이 advertised 상태이고 Seoul
standby는 drained 상태인지 확인합니다. service approval이 pending이거나 DNS가 해석되지 않으면
consumer를 전환하지 않습니다.

Service는 raw TCP proxy입니다. `verify-full`을 유지하려면 현재 primary의 PostgreSQL server
certificate가 기존 SAN을 보존하면서 `DNS:hololive-postgres.tail742dd8.ts.net`을 포함해야
합니다. standby를 재활성화할 때는 그 server certificate도 같은 service DNS를 포함해야
합니다. route probe용 전용
`route.pgpass`는 `0600 root:root`로 service DNS/5433과 `hololive_replicator`를 매치합니다.
인증서와 pgpass는 stack-secrets master에서 변경하고 host sync 뒤 각 PostgreSQL에 HUP을
보낸 다음 direct replication과 service endpoint를 모두 재검증합니다.

## Reactivation and retirement reference

물리 standby bootstrap, failover controller 설치, fencing·route hook, apply 활성화, HA smoke, 비상 실행과 폐기
절차는 지운 참고 자산과 함께 커밋 `bb000290e`의 이 문서에 남아 있습니다. 재활성화는 그 절차를 그대로 실행하지
않고 새로 승인한 정의와 검증으로 진행합니다.

중앙 `holo-postgres.container`는 fence 뒤 재시드한 구 primary를 unit으로 넘길 때 복제 접속 설정이 그대로
읽히도록 CA를 `/run/hololive-bot/certs/postgres-ca.pem`에 읽기 전용으로 마운트합니다. 그 standby의
`primary_conninfo`·`primary_slot_name`은 `ALTER SYSTEM`(data volume의 `postgresql.auto.conf`)으로 두고,
`sslrootcert`는 위 경로를, passfile은 data volume 안(`/var/lib/postgresql/replication.pgpass`, `0600 999:999`)을
씁니다. unit은 `/etc/stack-secrets/hololive-bot/postgres/pgpass`를 마운트하지 않습니다.

승격은 되돌릴 수 없습니다. 새 timeline으로 갈라진 구 primary는 그대로 재기동하지 않으며, fence 표식 삭제만으로
예전 데이터 디렉터리를 재기동하지 않습니다. 자동 failback은 지원하지 않습니다.

## Related documents

- `../DEPLOYMENT_BASELINE.md`
- `release.md`
- `rollback.md`
