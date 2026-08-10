# Runbook: postgres streaming replication and failover

## Role

중앙 primary(`hololive-osaka` / `100.100.1.8`)에서 Seoul AP 호스트(`iris-seoul` /
`100.100.1.5`)의 `holo-postgres-standby`로 가는 물리 스트리밍 복제와, standby에서
실행하는 fail-closed 자동 승격 컨트롤러의 운영 절차입니다. 두 호스트가 모두
`aarch64`라 물리 복제가 성립합니다.

정상 standby 상태는 `deploy/compose/docker-compose.standby.yml`이 소유합니다. 최초
`pg_basebackup`, fencing/route backend 승인, 자동 승격 활성화, 승격 뒤 재시딩은 이
문서가 소유합니다.

`kapu`의 `holo-postgres`는 이 복제와 무관합니다. `x86_64`라 물리 복제 대상이 될 수
없고, `hololive-db-backup.timer`가 매시 논리 덤프를 받는 백업 사본입니다.

## Safety model

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

기본 systemd unit은 `--dry-run`입니다. apply drop-in이 없으면 fencing, `pg_promote()`,
route 변경을 실행하지 않습니다. apply 모드도 fencing hook과 route hook 중 하나라도
없거나 안전 검증에 실패하면 승격 전에 중단합니다.

이 구조는 비동기 복제의 RPO를 0으로 만들지 않습니다. 마지막 정상 샘플 이후 primary에
commit됐지만 standby로 전송되기 전에 primary가 사라진 트랜잭션은 알 수 없습니다.
`MAX_KNOWN_LAG_BYTES=0`은 알려진 손실을 거부하는 방어선이지 synchronous commit 보장이
아닙니다.

## Dependencies

| 대상 | 내용 |
|---|---|
| primary `pg_hba.conf` | `deploy/compose/postgres/pg_hba.conf`. `hostssl replication hololive_replicator <standby-ip>/32`가 필요합니다. `host all all all`은 `replication` 유사 데이터베이스를 매치하지 않습니다. |
| primary 복제 역할 | `hololive_replicator`. init-db가 `HOLOLIVE_REPLICATOR_PASSWORD`가 있을 때만 만듭니다. |
| failover DB 권한 | `hololive_replicator`에 `hololive` CONNECT와 `pg_catalog.pg_promote(boolean, integer)` EXECUTE만 부여합니다. OS controller에 admin/superuser 자격을 주지 않습니다. |
| primary 복제 슬롯 | `iris_seoul_standby` physical slot. standby가 오래 끊기면 슬롯이 WAL을 보존해 primary 디스크가 찰 수 있습니다. |
| standby 자격 | `/etc/stack-secrets/hololive-bot/postgres/pgpass` (`0600 70:70`). PostgreSQL 컨테이너 uid 70이 읽을 수 있어야 합니다. |
| standby CA | `/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem`. `primary_conninfo`와 controller probe가 `sslmode=verify-full`을 사용합니다. |
| host client | PGDG `postgresql-client-18`의 canonical `/usr/lib/postgresql/18/bin/psql`. `/usr/bin/psql`의 `pg_wrapper` symlink는 trusted-path 검사에서 거부합니다. controller는 Docker socket 또는 `docker` 그룹을 사용하지 않습니다. |
| failover env | `/etc/stack-secrets/hololive-bot/postgres-failover.env` (`0600 root:root`). systemd `LoadCredential`이 전용 사용자에게 read-only 사본을 전달하고 allowlist launcher가 해석합니다. |
| fencing backend | 구 primary가 절대로 writer로 재등장하지 않게 하는 외부 증명입니다. SSH reference hook은 호스트가 reachable할 때만 유효합니다. 전원/호스트 상실까지 자동 처리하려면 hypervisor/cloud/PDU 같은 out-of-band fence hook이 필요합니다. |
| route backend | `HOLOLIVE_CENTRAL_POSTGRES_HOST/PORT`의 권위 owner 또는 안정적 VIP/DNS/proxy를 원자적으로 전환하고, 새 endpoint가 read/write인지 검증해야 합니다. |

## Bootstrap

primary 준비가 끝난 뒤 standby 호스트에서 실행합니다.

```bash
# 1. 볼륨 생성
docker volume create hololive-bot_holo-pg-standby-data

# 2. base backup. -R은 사용하지 않습니다. primary_conninfo 소유자는 Compose입니다.
docker run --rm \
  -v hololive-bot_holo-pg-standby-data:/var/lib/postgresql \
  -v /etc/stack-secrets/hololive-bot/certs/postgres-ca.pem:/run/hololive-bot/certs/postgres-ca.pem:ro \
  -v /etc/stack-secrets/hololive-bot/postgres/pgpass:/run/hololive-bot/postgres/pgpass:ro \
  --user 70:70 \
  -e PGPASSFILE=/run/hololive-bot/postgres/pgpass \
  -e PGSSLMODE=verify-full \
  -e PGSSLROOTCERT=/run/hololive-bot/certs/postgres-ca.pem \
  --entrypoint pg_basebackup \
  postgres:18.4-alpine \
  -h 100.100.1.8 -p 5433 -U hololive_replicator \
  -D /var/lib/postgresql/pgdata -X stream -S iris_seoul_standby -P -v

# 3. standby signal
docker run --rm -v hololive-bot_holo-pg-standby-data:/v --user 70:70 \
  --entrypoint sh postgres:18.4-alpine -c \
  'touch /v/pgdata/standby.signal'

# 4. 기동
cd /home/ubuntu/hololive-bot/deploy/compose
docker compose -p hololive-bot -f docker-compose.standby.yml \
  up -d --no-build holo-postgres-standby
```

자동 route가 새 primary에 접속하려면 standby port를 tailnet에 미리 열어야 합니다.
`ap-compose.env`의 값은 명시적으로 승인합니다.

```text
HOLOLIVE_STANDBY_POSTGRES_BIND_IP=100.100.1.5
HOLOLIVE_STANDBY_POSTGRES_PORT=5434
```

방화벽은 필요한 tailnet source만 허용해야 합니다. public bind는 금지합니다.

## Install the failover controller

standby 호스트에 unit을 설치하되 처음에는 dry-run timer만 활성화합니다.

```bash
test -x /usr/lib/postgresql/18/bin/psql
/usr/lib/postgresql/18/bin/psql --version
sudo install -m0644 scripts/systemd/hololive-postgres-failover.sysusers.conf \
  /usr/lib/sysusers.d/hololive-postgres-failover.conf
sudo systemd-sysusers /usr/lib/sysusers.d/hololive-postgres-failover.conf
sudo install -d -m0755 /usr/local/libexec/hololive-postgres-failover/lib
sudo install -m0644 scripts/ops/postgres-failover-launch.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-failover-launch.sh
sudo install -m0644 scripts/ops/postgres-failover.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-failover.sh
sudo install -m0644 scripts/ops/lib/postgres-failover-lib.sh \
  /usr/local/libexec/hololive-postgres-failover/lib/postgres-failover-lib.sh
sudo install -m0644 scripts/ops/lib/postgres-failover-transition-lib.sh \
  /usr/local/libexec/hololive-postgres-failover/lib/postgres-failover-transition-lib.sh
sudo install -m0644 scripts/ops/postgres-failover-fence-ssh.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-failover-fence-ssh.sh
sudo install -m0644 scripts/ops/postgres-failover.service \
  /etc/systemd/system/postgres-failover.service
sudo install -m0644 scripts/ops/postgres-failover.timer \
  /etc/systemd/system/postgres-failover.timer
sudo install -m0600 scripts/ops/postgres-failover.env.example \
  /etc/stack-secrets/hololive-bot/postgres-failover.env
sudo systemctl daemon-reload
sudo systemctl enable --now postgres-failover.timer
```

`systemctl show postgres-failover.service -p User -p Group`은 둘 다
`hololive-pg-failover`여야 합니다. 이 계정을 `docker` 그룹에 넣지 않습니다. state는
`StateDirectory=`가 소유하고, pgpass/CA/SSH 파일은 unit과 apply drop-in의
`LoadCredential=`로만 전달합니다.

기존 primary에는 apply 활성화 전에 admin 세션으로 다음 privilege bootstrap을 별도 승인해
한 번 적용합니다. 이 SQL은 대상 role이 LOGIN+REPLICATION이면서 non-superuser인지 먼저
검증하고 CONNECT와 `pg_promote()` 실행 권한만 부여합니다.

```bash
psql -U postgres_admin -d postgres \
  -v failover_user=hololive_replicator \
  -v failover_database=hololive \
  -f scripts/maintenance/postgres-failover-db-role.sql
```

`journalctl -u postgres-failover.service`에서 `primary_healthy` 관측과 LSN이 반복 기록되는지
확인합니다. dry-run에서 장애 조건을 만족하면 `promotion_would_run`까지만 기록돼야 합니다.

### Fencing hook

controller는 절대경로의 regular file만 실행하며 symlink, group/world writable, 비root 소유
hook을 거부합니다. hook 파일은 root-owned이지만 프로세스는 `hololive-pg-failover`로 실행됩니다.
old-primary의 실제 `systemctl`/Docker 변경만 제한된 remote root helper가 담당합니다.

```text
POSTGRES_FAILOVER_REQUEST_ID
POSTGRES_FAILOVER_PRIMARY_HOST
POSTGRES_FAILOVER_PRIMARY_PORT
POSTGRES_FAILOVER_NEW_PRIMARY_HOST
POSTGRES_FAILOVER_NEW_PRIMARY_PORT
POSTGRES_FAILOVER_FAILURE_SINCE
POSTGRES_FAILOVER_LAST_PRIMARY_LSN

stdout: FENCED|<POSTGRES_FAILOVER_PRIMARY_HOST>|<POSTGRES_FAILOVER_NEW_PRIMARY_HOST>:<POSTGRES_FAILOVER_NEW_PRIMARY_PORT>|<POSTGRES_FAILOVER_REQUEST_ID>|<8..128-char-fence-token>
```

네 번째 필드는 현재 invocation ID와 정확히 일치해야 하며, 다섯 번째 필드는 같은 fence
generation에서 재호출해도 유지되는 durable token입니다. 이 분리로 예전 성공 응답 재사용을
거부하면서 crash recovery는 같은 fence generation에 묶입니다.

`postgres-failover-fence-ssh.sh`는 reference backend입니다. 구 primary에서
`postgres-primary-fence.sh`를 sudo로 실행해 다음을 수행합니다.

- `deunhealth`와 `holo-postgres`의 Docker restart policy를 먼저 `no`로 변경·검증
- `/var/lib/hololive-postgres-fence/fence.intent`를 영속 기록
- `hololive-compose.service` 정지
- `deunhealth`와 `holo-postgres`를 정지
- 정지와 restart policy를 재검증
- `/var/lib/hololive-postgres-fence/fenced` 영속 마커에 fence token과 새 primary 후보 endpoint 기록
- 이미 fenced된 세대를 다른 새 primary 후보가 재사용하려 하면 거부

구 primary에는 먼저 fence condition이 포함된 unit과 remote action을 배포합니다.

```bash
sudo install -d -m0755 /usr/local/libexec/hololive-postgres-failover
sudo install -m0644 scripts/ops/postgres-primary-fence.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-primary-fence.sh
sudo install -m0644 scripts/ops/postgres-primary-unfence.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-primary-unfence.sh
sudo install -m0644 scripts/systemd/hololive-compose.service \
  /etc/systemd/system/hololive-compose.service
sudo systemctl daemon-reload
sudo systemctl cat hololive-compose.service | \
  grep -F 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced'
```

SSH 사용자는 고정 remote script만 sudo할 수 있어야 합니다. 아래 `<fence-user>`와 경로는 실제
배포 계정에 맞추고 `visudo -cf`로 검증합니다.

```text
<fence-user> ALL=(root) NOPASSWD: /usr/bin/env bash /usr/local/libexec/hololive-postgres-failover/postgres-primary-fence.sh *
```

`hololive-compose.service`는 intent/fenced 마커 중 하나라도 있으면 시작되지 않습니다.
이 SSH backend는 구 primary가 reachable한 장애에만 성공합니다. 호스트 전원 상실에서 SSH가
실패하면 controller도 fail-closed로 승격하지 않습니다. 완전 자동 host-loss failover에는
동일 acknowledgement 계약을 구현한 외부 전원 fencing hook을 사용합니다.

### Route hook

route hook은 다음 입력을 받습니다.

```text
POSTGRES_FAILOVER_OLD_PRIMARY_HOST
POSTGRES_FAILOVER_OLD_PRIMARY_PORT
POSTGRES_FAILOVER_NEW_PRIMARY_HOST
POSTGRES_FAILOVER_NEW_PRIMARY_PORT
POSTGRES_FAILOVER_FENCE_TOKEN

stdout: ROUTED|<POSTGRES_FAILOVER_NEW_PRIMARY_HOST>:<POSTGRES_FAILOVER_NEW_PRIMARY_PORT>|<POSTGRES_FAILOVER_FENCE_TOKEN>
```

hook은 endpoint owner를 갱신한 뒤 새 주소에서 `pg_is_in_recovery()=false`와
`transaction_read_only=off`를 검증하고 acknowledgement를 반환해야 합니다. 단순 파일 수정만
하고 소비자 재기동/재연결을 생략하면 route 완료로 인정하면 안 됩니다. 승격은 완료됐지만
route hook이 실패하면 controller는 `promoted_route_failed`를 저장하고 다음 timer에서 route만
재시도하며 두 번째 `pg_promote()`는 실행하지 않습니다. acknowledgement의 세 번째 필드는
fencing generation을 묶기 위해 입력받은 fence token과 정확히 같아야 합니다. hook은
멱등해야 하며 timeout이나 controller crash 뒤 같은 fence token으로 재호출돼도 endpoint를
재적용·재검증한 후 동일한 acknowledgement를 반환해야 합니다.

### Enable apply

fence와 route의 fault-injection 검증이 끝난 뒤에만 apply drop-in을 설치합니다.

```bash
sudo install -d -m0755 /etc/systemd/system/postgres-failover.service.d
sudo install -m0644 scripts/ops/postgres-failover-apply.conf.example \
  /etc/systemd/system/postgres-failover.service.d/apply.conf
sudo systemctl daemon-reload
sudo systemctl restart postgres-failover.timer
```

## Smoke test

```bash
# primary: streaming과 slot
# primary host에서 실행
docker exec holo-postgres psql -U postgres_admin -d hololive -Atc \
  "select application_name, state, sync_state, sent_lsn, replay_lsn, replay_lag from pg_stat_replication"
docker exec holo-postgres psql -U postgres_admin -d hololive -Atc \
  "select slot_name, active, pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) from pg_replication_slots"

# standby: recovery/read-only와 host promotion signal 부재
docker exec holo-postgres-standby psql -U postgres_admin -d hololive -Atc \
  "select pg_is_in_recovery(), current_setting('transaction_read_only'), pg_last_wal_replay_lsn()"
test ! -e /var/lib/hololive-postgres-failover/health.signal

# controller dry-run 상태
sudo systemctl start postgres-failover.service
sudo journalctl -u postgres-failover.service -n 20 --no-pager
sudo cat /var/lib/hololive-postgres-failover/state.tsv
```

합격 기준은 `state=streaming`, slot `active=true`, standby
`pg_is_in_recovery()=t`, replay lag 수 초 이내, controller `primary_healthy`입니다.
접속 성공만으로는 복제를 증명하지 못합니다.

자동 승격 fault-injection은 production primary를 끊지 말고 격리된 두 PostgreSQL
인스턴스에서 수행합니다. 최소 검증 항목은 fence invalid ack, fence 뒤 writable primary,
stale LSN sample, route failure/retry, promotion 직후 controller crash입니다.

## Common failure modes

| 증상 | 원인 | 대응 |
|---|---|---|
| `no pg_hba.conf entry for replication connection` | primary에 standby IP replication 규칙 없음 | `deploy/compose/postgres/pg_hba.conf` 배포 후 `holo-postgres` 재생성. `hba_file`은 postmaster 설정입니다. |
| `hostssl` 규칙이 보이지 않음 | 서버 `ssl=off` | `ssl=on` 조건에서 `pg_hba_file_rules`를 확인합니다. |
| primary 디스크 증가 | standby 단절 중 physical slot이 WAL 보존 | standby 복구 또는 승인 후 slot 제거. |
| standby 기동 거부 `max_connections` | standby 설정이 primary보다 작음 | primary 이상으로 맞춥니다. |
| `could not read password file` | pgpass 소유자가 uid 70이 아님 | `0600 70:70`으로 sync합니다. |
| `promotion_blocked reason=no_healthy_observation` | exact-lag 정상 샘플을 아직 저장하지 못함 | 복제를 정상화하고 `primary_healthy`가 기록될 때까지 승격하지 않습니다. |
| `fence_failed` | SSH-only fence에서 구 host가 unreachable이거나 외부 fence 실패 | 구 primary 상태를 수동 확인하거나 out-of-band fence backend를 복구합니다. fencing을 우회하지 않습니다. |
| `old_primary_still_writable_after_fence` | fence가 거짓 성공했거나 다른 endpoint를 막음 | 즉시 hook을 비활성화하고 구 primary를 실제 격리합니다. |
| promoted container unhealthy | host health signal이 없거나 role과 marker 불일치 | controller intent/promoted marker를 확인합니다. 수동으로 signal만 만들지 말고 controller crash recovery를 실행합니다. |
| `promoted_route_failed` | DB 승격 후 endpoint 전환 실패 | 새 primary는 유지하고 route hook을 수정합니다. timer가 route만 재시도합니다. |

## Emergency execution

구 primary를 out-of-band로 이미 격리했더라도 `pg_promote()`를 직접 호출하지 않습니다.
승격 intent, host health signal, route 재시도 상태를 controller 한 곳이 소유해야 crash recovery가
성립합니다. 외부 fence hook이 기존 격리 상태를 멱등하게 확인해 acknowledgement를 반환하도록 한
뒤 apply controller를 1회 실행합니다.

```bash
sudo systemctl stop postgres-failover.timer
sudo systemctl start postgres-failover.service
sudo systemctl start postgres-failover.timer
```

연속 실패, 최소 outage, 마지막 정상 LSN freshness 조건을 우회하지 않습니다. 조건을 충족하지
못하면 데이터 손실 가능성을 운영자가 먼저 판단하고 별도 승인된 복구 절차를 작성해야 합니다.
구 primary 상태가 불명확한 채 직접 승격하면 split brain입니다.

## Rollback

승격은 되돌릴 수 없습니다. 새 timeline으로 갈라진 구 primary는 그대로 재기동하지 않습니다.

standby 구성을 폐기할 때는 slot까지 제거합니다.

```bash
# standby host
docker compose -p hololive-bot -f docker-compose.standby.yml down
docker volume rm hololive-bot_holo-pg-standby-data

# current primary
psql -Atc "select pg_drop_replication_slot('iris_seoul_standby')"
```

fenced 구 primary를 재사용하려면 새 primary에서 다시 base backup해 standby로 만든 뒤,
restart policy가 `no`인 상태로 수동 기동합니다. marker를 직접 삭제하지 않습니다. helper가
local recovery/read-only, WAL receiver `streaming`, sender endpoint, current primary read/write,
fence token을 모두 재검증한 뒤에만 boot fence를 제거합니다.

```bash
sudo /usr/bin/bash \
  /usr/local/libexec/hololive-postgres-failover/postgres-primary-unfence.sh \
  <fence-token> 100.100.1.8 100.100.1.5 5434
sudo systemctl reset-failed hololive-compose.service
```

marker 삭제만으로 예전 데이터 디렉터리를 재기동하면 안 됩니다. 자동 failback은 지원하지
않습니다.

## Related documents

- `../DEPLOYMENT_BASELINE.md`
- `release.md`
- `rollback.md`
- `../../../scripts/ops/postgres-failover.env.example`
