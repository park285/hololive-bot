# Runbook: postgres streaming replication

## Role

중앙 primary(`hololive-osaka` / `100.100.1.8`)에서 Seoul AP 호스트(`iris-seoul` /
`100.100.1.5`)의 `holo-postgres-standby`로 가는 **물리 스트리밍 복제**의 부트스트랩·검증·
승격 절차입니다. 두 호스트가 모두 `aarch64`라 물리 복제가 성립합니다.

정상 상태는 `deploy/compose/docker-compose.standby.yml`이 소유합니다. 이 문서는 compose가
표현할 수 없는 일회성 작업(최초 `pg_basebackup`, 승격, 재시딩)만 다룹니다.

`kapu`의 `holo-postgres`는 이 복제와 무관합니다 — `x86_64`라 복제 대상이 될 수 없고,
`hololive-db-backup.timer`가 매시 논리 덤프를 받는 **백업 사본**입니다.

## Dependencies

| 대상 | 내용 |
|---|---|
| primary `pg_hba.conf` | `deploy/compose/postgres/pg_hba.conf`. `hostssl replication hololive_replicator <standby-ip>/32` 줄이 없으면 원격 standby가 붙지 못합니다 — `host all all all`은 `replication` 유사 데이터베이스를 매치하지 않습니다. |
| primary 복제 역할 | `hololive_replicator`. `init-db`가 `HOLOLIVE_REPLICATOR_PASSWORD`가 있을 때만 만듭니다. |
| primary 복제 슬롯 | `iris_seoul_standby` (physical). 슬롯이 WAL을 잡아두므로 standby가 오래 끊기면 primary 디스크가 찹니다. |
| standby 자격 | `/etc/stack-secrets/hololive-bot/postgres/pgpass` (`0600 70 70`). PostgreSQL이 컨테이너 안에서 uid 70으로 돌기 때문에 소유자가 `70:70`이어야 읽힙니다. |
| standby CA | `/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem`. `primary_conninfo`가 `sslmode=verify-full`이라 primary 서버 인증서의 SAN에 primary tailnet IP가 있어야 합니다. |

## Bootstrap (일회성)

primary 쪽 준비가 끝난 뒤(역할·슬롯·`pg_hba` 배포 완료) standby 호스트에서 실행합니다.

```bash
# 1. 볼륨 생성
docker volume create hololive-bot_holo-pg-standby-data

# 2. base backup — -R 을 쓰지 않습니다.
#    -R 은 primary_conninfo 를 postgresql.auto.conf 에 써넣는데, auto.conf 는 커맨드라인
#    -c 보다 나중에 읽혀 compose 정의를 덮어씁니다. 소유자를 compose 하나로 유지합니다.
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

# 3. standby 모드 진입 신호
docker run --rm -v hololive-bot_holo-pg-standby-data:/v --user 70:70 \
  --entrypoint sh postgres:18.4-alpine -c 'touch /v/pgdata/standby.signal'

# 4. 기동
cd /home/ubuntu/hololive-bot/deploy/compose
docker compose -p hololive-bot -f docker-compose.standby.yml up -d --no-build holo-postgres-standby
```

## Smoke test

```bash
# primary: 스트리밍 상태와 지연
docker exec holo-postgres psql -U postgres_admin -d hololive -Atc \
  "select application_name, state, sync_state, sent_lsn, replay_lsn, replay_lag from pg_stat_replication"
docker exec holo-postgres psql -U postgres_admin -d hololive -Atc \
  "select slot_name, active, pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) from pg_replication_slots"

# standby: recovery 상태
docker exec holo-postgres-standby psql -U postgres_admin -d hololive -Atc \
  "select pg_is_in_recovery(), pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn()"
```

합격 기준: `state=streaming`, 슬롯 `active=true`, `pg_is_in_recovery()=t`, primary의
`pg_current_wal_lsn()`과 standby의 `pg_last_wal_replay_lsn()`이 일치(또는 지연이 수 초 내).

전파를 실제로 증명하려면 primary에 임시 테이블을 만들어 넣고 standby에서 읽은 뒤 지웁니다.
접속 성공만으로는 복제 중임을 증명하지 못합니다 — `pg_is_in_recovery()`까지 확인해야 합니다.

## Common failure modes

| 증상 | 원인 | 대응 |
|---|---|---|
| standby 로그 `FATAL: no pg_hba.conf entry for replication connection` | primary의 `pg_hba.conf`에 standby IP 규칙 없음 | `deploy/compose/postgres/pg_hba.conf`를 primary에 배포하고 `holo-postgres`를 재생성합니다. `hba_file`은 `PGC_POSTMASTER`라 reload로는 반영되지 않습니다. |
| `hostssl` 규칙이 `pg_hba_file_rules`에 안 보임 | 서버의 `ssl=off` | `hostssl` 규칙은 SSL이 꺼져 있으면 조용히 건너뜁니다. 규칙을 검증할 때는 반드시 `ssl=on` 조건에서 확인합니다. |
| primary 디스크가 계속 찬다 | standby가 끊긴 채 슬롯이 WAL을 붙잡음 | `pg_replication_slots`의 retained bytes를 확인하고, standby를 복구하거나 슬롯을 드롭합니다. |
| standby 기동 거부 `hot standby is not possible because max_connections < ...` | standby의 설정값이 primary보다 작음 | `max_connections` 등 primary와 같거나 크게 맞춥니다. |
| `pg_basebackup: could not read password file` | `pgpass` 소유자가 uid 70이 아님 | 호스트 manifest에서 `0600 70 70`으로 두고 다시 sync합니다. |

## Rollback

standby를 제거해도 primary는 영향을 받지 않습니다. 단, **슬롯을 반드시 함께 드롭**해야
primary가 WAL을 무한정 붙잡지 않습니다.

```bash
# standby 호스트
docker compose -p hololive-bot -f docker-compose.standby.yml down
docker volume rm hololive-bot_holo-pg-standby-data

# primary 호스트 — 슬롯 회수
docker exec holo-postgres psql -U postgres_admin -d hololive -Atc \
  "select pg_drop_replication_slot('iris_seoul_standby')"
```

재시딩이 필요하면 위 Bootstrap을 처음부터 다시 실행합니다(슬롯은 남겨둡니다).

## Promotion (primary 상실 시)

승격은 되돌릴 수 없습니다 — 승격한 순간 standby는 새 timeline으로 갈라져 구 primary의
WAL을 더 받을 수 없습니다. 구 primary가 살아 돌아올 가능성이 있으면 먼저 확인합니다.

```bash
docker exec holo-postgres-standby psql -U postgres_admin -d hololive -Atc "select pg_promote(true, 60)"
docker exec holo-postgres-standby psql -U postgres_admin -d hololive -Atc "select pg_is_in_recovery()"   # f
```

승격 후에는 소비자 주소(`POSTGRES_HOST`)를 새 primary로 돌리고, `hololive-db-backup`의
`HOLOLIVE_BACKUP_PRIMARY`도 함께 갱신합니다. standby가 tailnet에서 접속을 받으려면
`ports`를 loopback에서 tailnet 주소로 바꿔야 합니다 — 서버 인증서에는 이미 해당 호스트의
tailnet IP가 SAN으로 들어 있습니다.

## Related documents

- `../DEPLOYMENT_BASELINE.md`
- `release.md`
- `rollback.md`
