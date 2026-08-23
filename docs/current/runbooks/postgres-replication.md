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
| route backend | Tailscale Service `svc:hololive-postgres`, client endpoint `hololive-postgres.tail742dd8.ts.net:5433`. old primary drain 뒤 새 primary의 tailnet IP/port를 광고하고 stable endpoint가 read/write인지 검증합니다. |

## Stable database endpoint

모든 중앙·AP consumer는 다음 endpoint를 사용합니다. PostgreSQL container 내부 port와
primary/standby 복제 주소는 그대로 유지합니다.

```text
HOLOLIVE_CENTRAL_POSTGRES_HOST=hololive-postgres.tail742dd8.ts.net
HOLOLIVE_CENTRAL_POSTGRES_PORT=5433
```

먼저 Tailscale admin console의 Services에서 `hololive-postgres`를 만들고 허용 endpoint로
`tcp:5433`을 정의합니다. service DNS가 생성되기 전이나 approval이 pending인 상태에서는 다음
단계로 진행하지 않습니다.

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
policy test를 통과시킨 뒤 적용합니다. 두 DB host가 `tag:hololive-db`를 광고하도록 승인한 뒤
다음 순서로 service를 준비합니다.

```bash
# standby: 구성 직후 drain. consumer 전환 전이라 read-only endpoint가 노출되지 않습니다.
sudo tailscale serve --yes --service=svc:hololive-postgres --tcp=5433 tcp://100.100.1.5:5434
sudo tailscale serve drain svc:hololive-postgres

# current primary: 유일한 advertised service host
sudo tailscale serve --yes --service=svc:hololive-postgres --tcp=5433 tcp://100.100.1.8:5433
```

`tailscale serve get-config --all`에서 두 host의 target을 각각 확인하고, standby는 drained,
primary만 advertised 상태여야 합니다. service approval이 pending이거나 DNS가 해석되지 않으면
consumer를 전환하지 않습니다.

Service는 raw TCP proxy입니다. `verify-full`을 유지하려면 primary와 standby의 PostgreSQL
server certificate 모두 기존 SAN을 보존하면서
`DNS:hololive-postgres.tail742dd8.ts.net`을 포함해야 합니다. route probe용 전용
`route.pgpass`는 `0600 root:root`로 service DNS/5433과 `hololive_replicator`를 매치합니다.
인증서와 pgpass는 stack-secrets master에서 변경하고 host sync 뒤 각 PostgreSQL에 HUP을
보낸 다음 direct replication과 service endpoint를 모두 재검증합니다.

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
  postgres:18.6-alpine \
  -h 100.100.1.8 -p 5433 -U hololive_replicator \
  -D /var/lib/postgresql/pgdata -X stream -S iris_seoul_standby -P -v

# 3. standby signal
docker run --rm -v hololive-bot_holo-pg-standby-data:/v --user 70:70 \
  --entrypoint sh postgres:18.6-alpine -c \
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
sudo install -m0644 scripts/ops/postgres-failover-route-ssh.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-failover-route-ssh.sh
sudo install -m0644 scripts/ops/postgres-route-tailscale.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-route-tailscale.sh
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
`LoadCredential=`로만 전달합니다. controller는 systemd의 `0440 root:root` pgpass를
검증한 뒤 service 전용 `RuntimeDirectory=`에 `0600` 임시 passfile로 복사하고 종료 시
삭제합니다. 영속 `StateDirectory=`에는 credential을 기록하지 않습니다.

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

- `hololive-compose.service`의 현재 loaded boot-fence condition 검증
- `/var/lib/hololive-postgres-fence/fence.intent`를 첫 외부 변경 전에 영속 기록
- `deunhealth`와 `holo-postgres`의 Docker restart policy를 `no`로 변경·검증
- stable Tailscale Service를 drain
- 중앙 consumer는 stable endpoint에 연결된 채 유지하고 `deunhealth`와 `holo-postgres`만 정지
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
sudo install -m0755 scripts/ops/postgres-failover-ssh-dispatch.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-failover-ssh-dispatch.sh
sudo install -m0644 scripts/systemd/hololive-compose.service \
  /etc/systemd/system/hololive-compose.service
sudo systemctl daemon-reload
sudo systemctl cat hololive-compose.service | \
  grep -F 'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced'
```

SSH 사용자는 root-owned `AuthorizedKeysFile`과 server-side `ForceCommand`로 고정 remote
script 외의 명령을 실행할 수 없어야 합니다. `restrict`만으로는 명령 실행을 제한하지
못하므로 단독으로 사용하지 않습니다. 현재 전용 계정은 `hololive-pg-fence`이며 password
login과 forwarding은 허용하지 않습니다.

```text
hololive-pg-fence ALL=(root) NOPASSWD: /usr/bin/env bash /usr/local/libexec/hololive-postgres-failover/postgres-primary-fence.sh *
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

reference backend는 `postgres-failover-route-ssh.sh`가 새 primary host의 전용
`hololive-pg-route` 계정으로 접속하고, root helper `postgres-route-tailscale.sh`만 sudo로
실행합니다. helper는 다음을 모두 만족해야 `ROUTED`를 반환합니다.

- new primary host가 로컬 `tailscale0` IPv4와 정확히 일치
- `svc:hololive-postgres`의 TCP 5433 target이 `tcp://<new-primary>:<new-port>`와 일치하고 advertised 상태
- service DNS를 통한 `verify-full` probe가 정확히 `f|off`
- root-owned route config, `route.pgpass`, CA, executable, state path 검증
- 같은 fence token 재호출에서도 route 재적용·probe 수행

standby host 설치 예시는 다음과 같습니다.

```bash
sudo install -m0644 scripts/systemd/hololive-postgres-route.sysusers.conf \
  /usr/lib/sysusers.d/hololive-postgres-route.conf
sudo systemd-sysusers /usr/lib/sysusers.d/hololive-postgres-route.conf
sudo install -d -m0755 -o root -g root /var/empty
sudo usermod --home /var/empty --shell /bin/dash hololive-pg-route
getent passwd hololive-pg-route | grep -F ':/var/empty:/bin/dash'
sudo install -d -m0755 /etc/hololive-postgres-failover
sudo install -m0600 scripts/ops/postgres-route-tailscale.env.example \
  /etc/hololive-postgres-failover/route.env
sudo install -d -m0700 /var/lib/hololive-postgres-route
sudo install -m0644 scripts/ops/postgres-route-tailscale.sudoers \
  /etc/sudoers.d/hololive-postgres-route
sudo visudo -cf /etc/sudoers.d/hololive-postgres-route
```

primary에는 같은 방식으로 `hololive-postgres-fence.sysusers.conf`와
`postgres-primary-fence.sudoers`를 설치합니다. 두 호스트에는 dispatcher와 sshd Match 설정을
설치하고, 각 public key는 root-owned 전용 파일에 `restrict` option과 함께 한 줄만 둡니다.
사용자 home의 `.ssh/authorized_keys`는 사용하지 않습니다. 두 계정의 home은 root-owned
`/var/empty`, login shell은 `/bin/dash`로 고정합니다. 기존 계정이라면 `usermod` 적용 전에
기존 home의 `.bashrc`, profile, `.ssh/environment` 존재 여부를 검사하고 사용하지 않는다는
것을 확인합니다.

```bash
sudo install -m0755 scripts/ops/postgres-failover-ssh-dispatch.sh \
  /usr/local/libexec/hololive-postgres-failover/postgres-failover-ssh-dispatch.sh
sudo install -d -m0755 -o root -g root /var/empty
sudo usermod --home /var/empty --shell /bin/dash <hololive-pg-fence-or-hololive-pg-route>
sudo install -d -m0755 -o root -g root /etc/ssh/authorized_keys
sudo install -m0600 -o root -g root <authorized-key-file> \
  /etc/ssh/authorized_keys/<hololive-pg-fence-or-hololive-pg-route>
sudo install -m0644 -o root -g root scripts/ops/hololive-postgres-failover.sshd.conf \
  /etc/ssh/sshd_config.d/90-hololive-postgres-failover.conf
sudo sshd -t
sudo sshd -T -C user=hololive-pg-fence,host=localhost,addr=127.0.0.1 | \
  grep -E '^(authorizedkeysfile|forcecommand|disableforwarding) '
sudo sshd -T -C user=hololive-pg-route,host=localhost,addr=127.0.0.1 | \
  grep -E '^(authorizedkeysfile|forcecommand|disableforwarding) '
sudo sshd -T -C user=hololive-pg-route,host=localhost,addr=127.0.0.1 | \
  grep -E '^(permituserenvironment|acceptenv) '
sudo systemctl reload ssh
```

private key와 pinned `known_hosts`는 controller의 `LoadCredential=` 경로로만 배포합니다. 두
키는 서로 재사용하지 않습니다. `sshd -T` 결과는 각 계정의 root-owned key path, 해당
dispatcher mode, `disableforwarding yes`와 정확히 일치해야 합니다. 임의 명령 probe는 실패해야
하고, `permituserenvironment no`여야 하며 `acceptenv`에는 `BASH_ENV`, `ENV`, `PATH`가 없어야
합니다. 정상 controller command만 acknowledgement를 반환해야 합니다.

route config/state와 credential 경로는 다음으로 고정합니다.

```text
/etc/hololive-postgres-failover/route.env                 root:root 0600
/etc/stack-secrets/hololive-bot/postgres-failover/route.pgpass root:root 0600
/var/lib/hololive-postgres-route                         root:root 0700
```

### Isolated apply verification

production apply를 활성화하기 전에 repository controller를 실제 PostgreSQL 18.4 두 node에
연결하는 격리 harness를 실행합니다. 고유한 internal Docker network와 volume만 사용하며
production failover 환경변수와 remote Docker context가 있으면 중단합니다.

```bash
HOLOLIVE_POSTGRES_FAILOVER_INTEGRATION=1 \
  bash scripts/ops/integration/postgres-failover-integration_test.sh
```

physical slot, TLS `verify-full`, zero-known-lag sample, durable fence, old primary 비쓰기,
`pg_promote`, new primary `f|off`, route 멱등성, cleanup residue 0이 모두 통과해야 합니다.

### Enable apply

fence와 route의 fault-injection 검증이 끝난 뒤에만 apply drop-in을 설치합니다.

```bash
sudo install -d -m0755 /etc/systemd/system/postgres-failover.service.d
sudo install -m0644 scripts/ops/postgres-failover-apply.conf.example \
  /etc/systemd/system/postgres-failover.service.d/apply.conf
sudo systemctl daemon-reload
sudo systemctl restart postgres-failover.timer
```

활성화 직후에는 production 장애를 주입하지 않습니다. primary/standby가 정상인 상태에서
controller를 1회 실행하고 `primary_healthy`, `failure_count=0`,
`promotion_state=monitoring`만 확인합니다. apply drop-in, SSH credentials, route config,
Tailscale approval 중 하나라도 불완전하면 drop-in을 제거하고 dry-run으로 복귀합니다.

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

consumer stable endpoint 전환은 service endpoint의 `f|off`가 확인된 뒤 중앙과 AP를 한 host씩
재배포합니다. 각 단계에서 readiness, restart count, DB TLS 접속을 확인하고 다음 host로
진행합니다. old primary direct endpoint는 rollback용으로 유지하되 신규 consumer 설정에는
남기지 않습니다.

apply 활성화 뒤 fencing은 전체 central Compose stack을 내리지 않습니다. 이미 stable endpoint로
기동한 API, alarm worker, producer는 old PostgreSQL과 autoheal만 정지하는 동안 실행을 유지하고
route 전환 뒤 재접속합니다. fence marker가 있는 동안에는 local DB `depends_on` 때문에 central
stack 전체 재배포를 시도하지 않습니다. old host를 새 primary의 streaming standby로 재시딩하고
unfence한 뒤에만 정상 Compose lifecycle을 재개합니다.

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
| `cannot drain Tailscale Service` | primary service가 미승인·미구성됐거나 tailscaled 오류 | DB stop 전에 중단된 상태입니다. service 승인/config를 복구하고 같은 fence generation을 재실행합니다. |
| `Tailscale service PostgreSQL probe failed` | service DNS/ACL/certificate/pgpass/advertisement 불일치 | 새 primary와 old-primary fence를 유지하고 route만 복구합니다. direct old endpoint로 되돌리지 않습니다. |

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

승격 전 rollout rollback은 apply drop-in을 제거하고 `daemon-reload` 뒤 timer를 dry-run으로
재시작하는 것으로 시작합니다. consumer가 stable service를 이미 사용 중이면 primary service가
정상 `f|off`인 동안 그대로 유지할 수 있습니다. direct endpoint로 되돌려야 할 때는 모든
consumer host를 순차 복구하고 readiness를 확인한 다음에만 primary의 service advertisement를
drain/clear합니다. standby service는 계속 drained 상태로 둡니다.

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
fence token을 모두 재검증한 뒤에만 boot fence를 제거합니다. consumer를 유지한 새 lifecycle에서는
`hololive-compose.service`가 `active/exited`여도 허용하며, `inactive/dead`도 허용합니다. 그 외
transitioning state나 `NeedDaemonReload=yes`는 거부하므로 unit을 억지로 stop하지 않습니다.
helper는 marker 제거 전에 PostgreSQL과 `deunhealth`의 restart policy를 `always`로 복원하고
`deunhealth`를 기동·검증합니다. 이 reconciliation이 실패하면 둘을 다시 `restart=no`로 만들고
fence marker를 유지합니다.

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
