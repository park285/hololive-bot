# 중앙 Quadlet unit

중앙 호스트(hololive-seoul, `deploy/topology/hosts.conf`의 `central`)는 rootful Podman과 systemd
Quadlet으로 운영합니다. iris-seoul collector b는 같은 방식의 별도 unit([deploy/podman/iris-seoul](../podman/iris-seoul/README.md))이며
배포·롤백은 중앙 c와 같은 `po-central-cutover.sh`(slot `b`)가 소유합니다.

## 구성

| 경로 | 내용 |
| --- | --- |
| `hololive-bot/` | 중앙 운영 unit. 호스트의 `/etc/containers/systemd/hololive-bot/`에 설치합니다 |
| `optional/x-spaces/` | `HOLOLIVE_X_SPACES_ENABLED=1`일 때만 설치하는 drop-in(`40-x-spaces.conf`) |
| `optional/schedule-poster/` | `HOLOLIVE_SCHEDULE_POSTER_ENABLED=1`일 때만 설치하는 hololive-api drop-in(`41-schedule-poster.conf`) |
| `optional/schedule-poster-review/` | `HOLOLIVE_SCHEDULE_POSTER_REVIEW_ENABLED=1`일 때만 설치하는 hololive-api drop-in(`42-schedule-poster-review.conf`, 후보 자동 검토용 Gemini API 키) |
| `optional/broadcast-type-decision/` | `HOLOLIVE_BROADCAST_TYPE_DECISION_ENABLED=1`일 때만 설치하는 hololive-api drop-in(`43-broadcast-type-decision.conf`, 미분류 방송 유형 판정용 Gemini API 키) |
| `optional/event-grade/` | `HOLOLIVE_EVENT_GRADE_ENABLED=1`일 때만 설치하는 collector c drop-in(`44-event-grade.conf`, 이벤트 등급 수집용 게임 인증 파일) |

`optional/`은 설치 트리 밖에 둡니다. Quadlet은 같은 검색 루트 아래 형제 디렉터리의
`<unit>.container.d/` drop-in도 적용하므로, 선택 drop-in을 `hololive-bot/` 옆에 두면 항상 적용됩니다.

| unit | 컨테이너 | 비고 |
| --- | --- | --- |
| `holo-postgres.container` | `holo-postgres` | 권위 primary. `admin-dashboard-ingress-firewall.service`를 Requires |
| `valkey-cache.container` | `valkey-cache` | 중앙 TLS 서버. 서비스별 단계 승인이 없으면 설치·기동하지 않음 |
| `hololive-db-migrate.container` | `hololive-db-migrate` | one-shot. `[Install]` 없음 |
| `hololive-alarm-worker.container` | `hololive-alarm-worker` | 알림 dispatcher(단일 인스턴스) |
| `hololive-api.container` | `hololive-api` | bot/admin/YouTube plane |
| `hololive-youtube-po-c.container` | `hololive-youtube-po-c` | collector c의 격리 PO issuer |
| `hololive-collector-c.container` | `hololive-collector-c` | collector fleet c. network alias `collector` |
| `admin-dashboard-ingress.container` | `admin-dashboard-ingress` | host network shortlink ingress |
| `hololive-net.network` | `hololive-bot_hololive-net` | 172.23.0.0/16, interface `pm-holo` |

## 설치

배포할 commit의 clean checkout에서 root로 실행합니다. `/opt/hololive-bot/compose/current`는 `git archive` 결과라
git 저장소가 아니므로 거기서 실행하면 `repo git tree missing`으로 멈춥니다.

```bash
sudo ./scripts/deploy/sync-opt-current.sh
```

기존 운영 데이터·설정·권한을 바꾸지 않는 artifact-only 갱신에는
`sudo ./scripts/deploy/sync-opt-current.sh --preserve-runtime`을 사용합니다. 이 모드는 기존
`data`·`logs`·`backups`·`runtime-config` 디렉터리가 있어야 하며, runtime 입력을 backup/seed하지 않고
보호 디렉터리와 worker-profile 저장소의 bytes·owner·mode를 유지합니다. 기본 전체 동기화의 시드·권한 준비는
그 효과까지 승인된 설치에만 사용합니다. 두 모드 모두 새 고유 exec-tree 복구점을 만들며 기존 backup을
덮거나 보존 개수만으로 삭제하지 않습니다. 과거 복구점 정리는 별도 승인 절차입니다.

설치 도구는 다음을 확인한 뒤에만 unit을 설치합니다.

- hostname이 `kapu`이면 `HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1` 없이 거부합니다(두 번째 dispatcher 방지).
- `compose.env`의 `VALKEY_BIND_IP=100.100.1.8`을 API·alarm-worker가 TLS 직접 접속에 사용합니다. 전환 전에 이 주소의 `16379`가 최신 쓰기 primary인지 확인합니다.
- 앱 배포와 별개로 구 중앙 primary의 ACL·AOF와 실제 볼륨을 확인한 다음, `sudo ./scripts/deploy/sync-opt-current.sh --valkey-only`로 이 unit 하나만 교체할 수 있습니다. 서비스의 재기동·단계 승인은 수행하지 않고, 앱·DB·기존 데이터·실행 트리·백업을 변경하지 않습니다. [영속 단계와 최종 캐시](../compose/README.md#중앙-valkey)를 구분하십시오.
- `HOLOLIVE_X_SPACES_ENABLED`·`HOLOLIVE_SCHEDULE_POSTER_ENABLED`·`HOLOLIVE_SCHEDULE_POSTER_REVIEW_ENABLED`·`HOLOLIVE_BROADCAST_TYPE_DECISION_ENABLED`·`HOLOLIVE_EVENT_GRADE_ENABLED`는 `0` 또는 `1`이며, 그 값에 따라 각 선택 기능 drop-in을 설치하거나 지웁니다.
- `compose.env`의 PostgreSQL pool 값이 연결 예산(`scripts/deploy/lib/postgres-capacity.sh`)을 넘으면 거부합니다.
- 설치 전에 설치 뒤와 같은 구성(staged unit, 켜진 선택 기능 drop-in, 설치 도구가 건드리지 않는 기존 drop-in)이
  `quadlet -dryrun`을 통과해야 합니다. 실패하면 `/opt`·prestart·unit을 바꾸지 않습니다.
- 설치 뒤 실행 트리가 root 소유여야 합니다.

배포 트리에는 archive한 commit을 `.source-revision`으로 남깁니다. 배포 helper가 image revision을 이 값과 대조합니다.

방화벽 규칙(`scripts/systemd/admin-dashboard-ingress.nft`)이 바뀐 배포에서는 설치 뒤
`sudo systemctl reload admin-dashboard-ingress-firewall.service`로 적용합니다(아래 '기동·정지·의존').
이 규칙은 같은 호스트의 ChatBotGo(5432)·TwentyQ(5458) PostgreSQL 게시 포트도 loopback과 kapu(`bot_postgres_sources`)에서만 받습니다.
Tailscale은 tailscale0에서 전달하는 패킷을 masquerade하므로, 규칙이 없으면 PostgreSQL은 원격 접속을 모두 bridge gateway(`172.23.0.1`)로 봅니다.
`forward_preserve_postgres_source` 체인은 PostgreSQL 게시 `5433→5432`의 `postgres_sources` 접속에만 그 표시를 지웁니다. 그래서 PostgreSQL HBA가 AP별 tailnet 주소와 역할을 함께 판정합니다. 두 봇 DB 게시와 다른 게시 포트의 masquerade는 그대로 유지합니다.
두 봇의 DB unit(`chatbotgo-postgres`·`twentyq-postgres`)도 holo-postgres처럼 이 방화벽 unit을 `Requires=`하므로, 방화벽 unit이 시작에 실패하면 기동하지 않고 멈추면 함께 멈춥니다. reload는 두 DB에 영향을 주지 않습니다.

prestart helper는 `/usr/local/libexec/hololive-bot/quadlet-prestart`(+ `lib/`)로 설치됩니다.

## 호스트 값과 비밀

- 모든 unit이 `[Service] EnvironmentFile=/etc/stack-secrets/hololive-bot/compose.env`를 읽습니다.
  `[Service] Environment=`는 compose의 `${VAR:-기본값}` 기본값이며 compose.env가 우선합니다.
- compose.env는 `KEY=VALUE` 줄만 받습니다. 잘못된 key, 중복 key, 값의 앞뒤 공백은 prestart가 키 이름만 보고하고
  거부합니다. `KEY=` 빈 줄은 unit 기본값을 빈 값으로 덮으므로 기본값을 쓰려면 줄을 지웁니다.
- 비밀번호·API key는 `podman run` 인자에 남지 않도록 prestart가
  `/run/hololive-bot/quadlet-env/<container>.env`(디렉터리 0700, 파일 0600)에 써서 `EnvironmentFile=`로 넘깁니다.
  서비스별 env 파일(`bot.env`, `alarm-worker.env`, `collector.env`) 다음 순서라 compose의
  `environment` > `env_file` 우선순위를 유지합니다.
- PostgreSQL 로그인 비밀번호는 API `HOLOLIVE_DB_PASSWORD`, alarm-worker `HOLOLIVE_ALARM_WORKER_PASSWORD`,
  collector slot별 `HOLOLIVE_COLLECTOR_A_PASSWORD`~`D_PASSWORD`로 나누고 서로·`DB_PASSWORD`와 다르게 둡니다.
  이 창의 one-shot은 여섯 업무 역할의 로그인 비밀번호와 연결 한도를 변경하지 않습니다. 배포 이미지의 `db-migrate`는 창 전용 인자가 빠지면 DB 접속 전에 거부합니다. 신규 DB의 별도 초기화에서만 같은 이미지의 `--initialize`를 명시하며, 운영 one-shot은 `--schema-only`입니다.
  `--schema-only`가 대기 migration을 적용하려면 같은 트랜잭션에서 writer drain 가드의 superuser 세션과 그 advisory lock, login 게이트, 업무 역할 세션 0건이 모두 확인돼야 합니다. migrator 세션은 확인보다 먼저 `hololive-migration-window` 표식을 달고 통계 snapshot을 비운 뒤 확인하므로, 확인과 표식 사이에 가드가 끊겨도 그 틈에 로그인한 writer는 확인에서 드러납니다. 게이트는 가드 세션이 끊겨도 이 세션이 끝날 때까지 업무 역할 로그인을 막습니다.
  같은 비밀번호이면 기존 SCRAM verifier를 유지합니다. 실제 비밀번호를 바꿀 때는 해당 역할의 모든 앱 frontend를 먼저 drain·종료해야 하며, 비밀번호 변경 자체가 기존 세션을 폐기하지는 않습니다.
- compose에서 `:?`였던 필수 값(`LIVE_LOGS_PATH`,
  `POSTGRES_PORT_BIND_IP`, `VALKEY_BIND_IP`, `HOLOLIVE_API_PORT_BIND_IP`, `HOLOLIVE_ADMIN_API_PORT_BIND_IP`,
  `HOLOLIVE_BOT_PORT_BIND_IP`)은 prestart가 확인합니다. tailnet publish 주소는 `tailscale0`에 붙을 때까지 90초 기다립니다.
- `hololive-api`의 `APP_VERSION`은 배포 트리 `hololive/hololive-api/VERSION`에서 읽습니다.

## PostgreSQL 직접 TLS 접속

- 중앙 API·alarm-worker·collector c는 `holo-postgres:5432`, AP a·b·d는 `100.100.1.8:5433`에 `verify-full` TLS로 직접 접속합니다. 중앙 unit의 endpoint는 `HOLOLIVE_CENTRAL_POSTGRES_HOST`·`HOLOLIVE_CENTRAL_POSTGRES_PORT` 기본값입니다. `quadlet-redeploy-service.sh`는 effective 값이 이 endpoint와 `verify-full`이 아니면 거부합니다.
- `deploy/postgres/pg_hba.conf`는 역할마다 접속 source를 고정합니다. bridge(`172.16.0.0/12`)에서는 runtime·alarm-worker·collector c만, tailnet에서는 d `100.100.1.3/32`, b `100.100.1.5/32`, a `100.100.1.6/32`의 자기 역할만 받습니다. 관리자·migrator·nightly·복제 TCP는 거부합니다.
- 역할별 연결 한도는 `deploy/postgres/role-limits.sql`, 앱 풀 상한과 서버 예산은 `scripts/ci/postgres-capacity-policy.tsv`가 소유합니다. 배포 helper는 compose.env와 설치 unit·drop-in의 effective 풀 상한으로 같은 예산을 다시 계산합니다. collector 네 역할은 `@probe-reserve`로 한 연결을 남깁니다(역할 한도 7, 풀 상한 6). 전환 도구의 후보 `--check-database-endpoint`가 같은 역할로 직접 접속하므로, 실행 중인 collector 풀이 꽉 차도 검사가 한도에 걸리지 않습니다.
- 앱의 30초 `statement_timeout`은 연결 startup 파라미터로 전달합니다. 역할 단위 `ALTER ROLE … SET`은 쓰지 않습니다.
- 마이그레이션 창의 writer 차단은 PostgreSQL 19 login event trigger `hololive_writer_admission`이 맡습니다. `writer-drain-guard.py`가 관리 socket의 superuser 세션으로 `bootstrap_writer_admission.sql`을 멱등 적용합니다. 같은 DB에 `application_name`이 `hololive-writers-draining`/`hololive-writers-drained`인 superuser 세션이 있는 동안 여섯 업무 역할의 새 로그인은 `57P03`으로 거부됩니다. 가드는 업무 역할 backend가 0건인 것을 확인한 뒤에만 migrator를 시작합니다. trigger 함수가 고장 나면 superuser가 `-c event_triggers=off`로 접속해 복구합니다.

### PgBouncer 퇴역 전환

PgBouncer 경로로 운영 중인 배포를 직접 TLS로 옮기는 절차입니다. 모든 단계는 별도 운영 승인 아래에서만 실행합니다.

이 release는 퇴역 원점을 받지 않습니다. HBA에 pooler 주소 `172.23.255.254/32` 규칙이 없고, 중앙·c quiesce는 `holo-pgbouncer:6432` 원본을, b quiesce와 kapu d 도구는 6433 원점을 거부합니다. 따라서 1–9단계는 퇴역 원점 허용과 pooler 주소의 두 HBA 규칙(`hololive_pgbouncer_auth` 인증서, 여섯 업무 역할 SCRAM)이 남아 있는 1–9단계용 commit의 clean checkout과 그 commit의 문서로 실행합니다. 1–9단계용 commit은 `911ac8fdebfcb011951a320103f1caec83b9f14b`입니다. squash 병합하면 이 commit이 게시 이력에서 사라지므로, 이 변경은 commit을 보존하는 병합으로 게시하고 같은 SHA를 가리키는 보존 ref(게시 승인 때 이름을 정하는 태그나 브랜치)를 함께 둡니다. 실행 전에 새 clone에서 그 ref가 이 SHA로 풀리는지(`git rev-parse <보존 ref>^{commit}`) 확인합니다. 그보다 앞선 commit은 전환 도구의 수정이 빠져 있으므로 쓰지 않습니다. HBA는 단일 파일 bind라서 PostgreSQL을 다시 만들 때만 바뀌고, 중앙 hold는 PostgreSQL 실행 세대를 고정하므로 hold 안에서 PostgreSQL을 재시작할 수 없기 때문입니다. 9단계까지 끝낸 뒤 10단계에서 이 release를 설치하고 PostgreSQL을 다시 만들어 최종 HBA를 적용합니다.

중앙 `--resume`의 카드 창 가드는 여섯 업무 역할의 연결이 모두 0이어야 진행합니다. pooler의 idle backend도 역할 연결 한도를 차지합니다. 따라서 writer는 정지만 먼저 하고, pooler를 멈춘 뒤 중앙부터 재개합니다. collector는 정지부터 재개까지, API·alarm-worker는 중앙 정지부터 중앙 재개까지 멈춥니다.

1. 복구점: 각 writer의 실행 세대와 endpoint, `holo-postgres`·`holo-pgbouncer`의 CID·image ID·`StartedAt`, 현재 HBA mount, 적용된 migration 원장(`SELECT filename FROM public.schema_migrations ORDER BY filename`)을 기록합니다.
2. 중앙 설치: 1–9단계용 commit의 clean checkout에서 `sync-opt-current.sh`로 전체 설치합니다. 설치는 `holo-pgbouncer.container`를 지웁니다. 실행 중인 pooler는 정지할 때까지 계속 동작하므로 이후 재시작하지 않습니다. 6433 게시 규칙이 사라지는 방화벽 reload는 7단계까지 미룹니다.
3. PostgreSQL 재생성: `systemctl restart holo-postgres.service`로 새 HBA를 적용합니다. pooler는 `Wants=`로만 묶여 있어 함께 멈추지 않고 backend를 다시 연결합니다. `SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL`이 0이고 기존 writer가 정상인지 확인합니다.
4. writer 정지(d→b→a→c→중앙): 첫 정지 전에 관리 socket에서 `SELECT r.rolconnlimit - count(a.pid) FROM pg_roles r LEFT JOIN pg_stat_activity a ON a.usename = r.rolname WHERE r.rolname = 'hololive_collector_b' GROUP BY r.rolconnlimit`가 1 이상인지 확인합니다. b quiesce의 후보 직접 접속 검사가 이 한 자리를 씁니다. pooler 시절의 b는 pooler backend로 역할 한도 7을 모두 채울 수 있고, 검사가 실패하면 일회성 정지 승인과 복구점이 이미 소비된 뒤입니다. 0이면 정지를 시작하지 않습니다.
   - d: 1–9단계용 commit kapu 절차의 `kapu-d-cutover.py quiesce --from-pooler`
   - b: [iris-seoul 절차](../podman/iris-seoul/README.md)의 `po-central-cutover.sh b quiesce`
   - a: [collector 런북](../../docs/current/runbooks/collector.md#osaka-a-w1w2-분리-정지-창)의 `ap-host-native-deploy.sh osaka --quiesce W2`. W2는 원래 6433 원점만 받습니다.
   - c: `po-central-cutover.sh c quiesce-verified`
   - 중앙: 1단계 원장에 `002_epoch4_event_grade.sql`이 있으면 먼저 `SELECT count(*) FROM event_grade_regions r JOIN event_grade_consumer_offsets o USING (region) WHERE o.applied_seq < r.published_seq`가 0인지 확인합니다. 002 이전 head(epoch 3의 032, epoch 4의 001)에는 두 테이블이 없으므로 이 확인을 건너뜁니다. 원장에 002가 있는데 테이블이 없으면 진행하지 않습니다. collector가 모두 멈춘 뒤 worker가 남은 관측을 소비할 때까지 기다리며, 0이 아니면 005가 적용을 거부합니다. 이어서 `quadlet-redeploy-service.sh --quiesce <API image> <worker image>`. 실행 중 원본이 `holo-pgbouncer:6432`이면 설치 unit이 직접 TLS일 때만 정지하고, 원래 endpoint를 hold에 기록합니다.
5. pooler 정지: 여섯 hold가 모두 완료됐는지 확인한 뒤 `systemctl stop holo-pgbouncer.service`를 실행합니다. 이어서 `SELECT count(*) FROM pg_stat_activity WHERE client_addr = inet '172.23.255.254'`가 0인지 확인합니다. 0이 아니면 진행하지 않고 남은 세션의 출처를 조사합니다. backend를 강제 종료해 0을 만들지 않습니다.
6. 중앙 재개: `quadlet-redeploy-service.sh --resume <API release> <worker release>`. 가드가 writer 차단 게이트를 설치·확인하고 `--schema-only` migrator를 실행한 뒤 두 후보를 엽니다.
7. 방화벽: `sudo systemctl reload admin-dashboard-ingress-firewall.service`로 6433 게시 규칙을 지웁니다.
8. collector 재개(c→a→b→d): `po-central-cutover.sh c resume`, `ap-host-native-deploy.sh osaka --resume W2`, `po-central-cutover.sh b resume`, `kapu-d-cutover.py resume --from-pooler`. 각 후보는 활성화 전에 실제 경로의 `--check-database-endpoint`를 통과해야 합니다.
9. 정리(별도 승인): 관리 socket의 `hololive` DB에서 한 트랜잭션으로 `DROP SCHEMA hololive_pooler_auth CASCADE`, `REVOKE ALL ON DATABASE hololive FROM hololive_pgbouncer_auth`, `DROP ROLE hololive_pgbouncer_auth`를 실행하고 `pg_roles`에 역할이 남지 않았는지 확인합니다. `DROP ROLE`이 남은 권한 때문에 실패하면 트랜잭션 전체가 취소되므로, 남은 권한의 출처를 조사한 뒤 다시 실행합니다. 10단계 전까지 `hololive-net`의 `172.23.255.254`를 다른 컨테이너에 고정 배정하지 않습니다.
10. 최종 HBA: 이 release의 clean checkout에서 `sync-opt-current.sh`로 전체 설치하고, 승인된 PostgreSQL 재생성(`systemctl restart holo-postgres.service`)에서 새 HBA를 적용합니다. `pg_hba_file_rules`에 오류와 `172.23.255.254` 규칙이 없는지 확인합니다. AP W2 창은 osaka a의 일반 배포 창 표식과 묶여 있어 이 release에도 남아 있으며, W1/W2 창 체계를 퇴역할 때 함께 지웁니다.

어느 단계든 실패하면 hold와 복구 입력을 보존합니다. 이전 endpoint로의 rollback은 helper가 거부하므로 호환 후보로 fix-forward합니다.

### Epoch 4 배포 경계

후속 변경이 없는 20.0.0 one-shot은 기존 032 head나 새 기준선 1개를 **읽기만** 한다. 정상 032 DB에서는 DDL·업무 DML·원장·로그인 변경이 없다. 원래 PostgreSQL 세대·보호 inode·서비스와 writer endpoint를 그대로 보존한다. 19.2.1 source-only 창은 migration/보호 경로 변경을 거부하는 것이 정상이다. 20.0.0 전체 소스 staging, peer CAS, worker/collector별 이미지·프로필·실제 DB SQL 수용은 별도 릴리스 창에서 증명한다. 새 DB 초기화/구 DB 복구는 [규약](../../hololive/hololive-api/scripts/migrations/CONVENTIONS.md)을 따른다.

### writer 정지·재개 계약

중앙 API·worker와 b/c/d의 Quadlet은 **기동 차단 → 원래 unit으로 정지 → 최종 mask** 순서입니다. 활성 unit에 `/dev/null` mask를 먼저 로드하면 원래 `ExecStop`과 `KillMode`가 사라집니다. 반대로 mask의 reload만 미루면 기존 `Wants=`가 writer를 다시 기동할 수 있습니다.

같은 변경의 `systemd-stop-guard.py`가 각 unit에 설치하는 `90-hololive-stop-guard.conf`는 두 경로를 차단합니다.

- 명시적 기동·의존성·재부팅: 작업 소유 anchor가 있으면 `ConditionPathExists=!…`가 성립하지 않습니다.
- 프로세스 자체 종료: systemd의 자동 재시작은 위 조건을 다시 검사하지 않으므로 `Restart=no`, 빈 `RestartForceExitStatus=`가 실제 manager에 적용돼야 합니다.

어느 설정도 이미 실행 중인 프로세스를 정지시키지는 않습니다. 원래 정지 명령과 정책이 그대로인 상태에서 manager가 원 invocation을 정지했는지, 같은 시간 범위에 원래 Podman CID의 종료 이벤트가 단 한 건인지 확인합니다. 기동 차단 파일이나 컨테이너 부재만으로 정상 종료를 인정하지 않습니다. 부분 실패 시 guard·anchor·복구점·journal을 보존하고 임의 삭제, `reset-failed`, DB 변경, 다음 writer 기동을 하지 않습니다.
재개할 때 중앙과 b/c는 manager의 원래 start job·invocation 및 Podman `State.ConmonPid`와 unit `MainPID`의 일치를 확인합니다. 기동 클라이언트가 실패해도 요청을 반복하지 않습니다. 같은 job이 남았다면 가드 아래에서 해당 unit의 `TimeoutStartUSec` 범위로만 관찰합니다. 확인된 원래 writer는 정지하고, 교체된 컨테이너를 대신 정지하거나 후속 writer를 열지 않습니다.

`b/c quiesce`는 자기 mask의 inode와 두 컨테이너의 부재를 함께 확인합니다. `po-broker`는 의도적으로 SIGTERM을 다시 보내므로 Podman이 143, systemd가 `failed`를 기록할 수 있습니다. 성공한 `stop` 직전에 실행 중이던 바로 그 issuer invocation이고 두 PID가 0인 경우에만 이 상태를 차단으로 인정합니다. 실패 이력을 지우거나 collector의 오류를 정상 종료로 취급하지 않습니다. 종료 영수증이 없는 기존 hold에 영수증을 소급 작성하지 않습니다. `[OUTCOME] unconfirmed`이면 DB 변경·추가 기동을 중단하고 mask·원래 journal·복구점을 보존합니다.
정지 journal 조회가 실패하면 `[hold] stop journal unconfirmed`의 `unit`, `invocation`, `since`(epoch 마이크로초), `journal_status`를 보존합니다. 상태 0인데도 이 진단이 나오면 해당 범위에 일치한 이벤트가 없었다는 뜻입니다. 나중에 같은 이벤트를 찾더라도 원래 종료 결과나 영수증을 바꾸지 않습니다.

### 복구와 자격 증명 변경

- 중앙·b·native rollback은 현재 env/drop-in까지 반영한 endpoint를 이전 복구점과 비교합니다. 다르거나 해석되지 않으면 재기동 전에 거부합니다. 이전 HBA/DB 설정·effective endpoint를 별도 승인 아래 정합화한 뒤 기존 `--check`/`--apply`를 다시 실행합니다. native recovery guard도 기존 복구 절차로 해제하며 우회 flag는 없습니다.
- 실제 비밀번호 회전은 그 역할의 frontend를 먼저 종료하고 bootstrap한 뒤 새 비밀번호로 재접속합니다. 다른 역할은 verifier를 유지하므로 정상 재배포가 fleet 전체의 SCRAM 키를 무효화하지 않습니다. 만료/NOLOGIN도 새 인증 거부와 기존 세션 종료를 구분합니다.

## 기동·정지·의존

- 상시 unit은 `WantedBy=multi-user.target`이며 `Notify=healthy`라 start job은 healthy가 된 뒤 끝납니다.
- `HealthOnFailure=stop` + `Restart=always`가 자동 복구입니다. unhealthy container는 StopSignal(PostgreSQL `SIGINT`,
  ingress `SIGQUIT`, 나머지 `SIGTERM`)과 `StopTimeout`(api·worker 45초, PostgreSQL 120초)을 지켜 멈춘 뒤 systemd가 다시
  띄웁니다. `kill`(SIGKILL)은 배치 전송과 checkpoint를 끊으므로 쓰지 않습니다. collector c와 PO issuer는 예외(unhealthy만으로 멈추지 않음)입니다.
- `holo-postgres`는 `TimeoutStopSec=150`입니다. `ExecStop`(`podman rm`)이 `StopTimeout=120`을 다 쓰기 전에 systemd
  기본 90초가 끊지 않게 합니다.
- `Tmpfs=`에는 `noexec,nosuid,nodev`를 적습니다. Podman은 옵션을 준 `--tmpfs`에 Docker처럼 이 옵션을 붙이지 않습니다.
- 런타임 peer(PostgreSQL·Valkey·worker)는 `Wants=`+`After=`입니다. `Requires=`는 명시적 stop/restart를
  소비자에게 전파해 primary fence(소비자 유지)를 깨뜨리기 때문입니다.
  보안 전제인 `admin-dashboard-ingress-firewall.service`와 migration의 PostgreSQL 의존만 `Requires=`입니다.
- 방화벽 규칙 갱신은 `sudo systemctl reload admin-dashboard-ingress-firewall.service`로만 합니다. reload는 table
  선언·삭제·재정의를 한 nft transaction으로 적용해 규칙이 빈 순간이 없고, 새 파일이 거부되면 기존 table이 남습니다.
  `restart`·`stop`은 `Requires=`로 묶인 `holo-postgres`·`hololive-api`·`admin-dashboard-ingress`와 두 봇의
  `chatbotgo-postgres`·`twentyq-postgres`를 함께 재시작·정지합니다. reload는 전파되지 않습니다(systemd 257 user unit으로 실측).
- `stop` 뒤 방화벽 unit을 다시 `start`해도 함께 멈춘 unit은 다시 뜨지 않습니다. 방화벽 unit이 active인지 확인한 뒤
  `sudo systemctl start holo-postgres.service hololive-api.service admin-dashboard-ingress.service chatbotgo-postgres.service twentyq-postgres.service`로
  직접 기동하고 각 unit 상태를 확인합니다.
- 방화벽 unit을 없애려면 먼저 이 저장소와 두 봇 저장소 unit의 `Requires=`·`After=` 의존을 지우고 반영합니다.
  의존이 남은 채 unit만 지우면 다음 기동이 실패합니다.
- `hololive-api`를 시작하면 `hololive-alarm-worker`도 함께 시작됩니다(Wants).
- collector c는 compose.env의 `HOLOLIVE_DISABLE_COLLECTOR=1`이면 `ExecCondition`으로 건너뜁니다.

```bash
sudo systemctl status hololive-api.service
sudo systemctl show hololive-api.service -p NRestarts   # 마지막 명시적 restart 뒤 자동 재시작 횟수
sudo journalctl -u hololive-api.service --since -15m
```

API·worker의 image 교체와 migration은 아래 '배포 helper'로만 합니다.

## Primary fence

모든 unit은 `/var/lib/hololive-postgres-fence/fence.intent`·`fenced` 중 하나라도 있으면 시작하지 않습니다
(`ConditionPathExists=!`). `quadlet-redeploy-service.sh`와 `quadlet-rollback-service.sh`도 표식이 있으면 진행을
거부합니다. 표식을 쓰고 치우던 failover fence·unfence helper는 저장소에서 지웠으므로(커밋
`bb000290e`에 남아 있는 git 이력) 지금은 표식을 만드는 운영 경로가 없습니다. 표식이 보이면 직접 지우지 말고 원인을
확인한 뒤 별도 승인으로 처리합니다.

표식 없이 `systemctl stop holo-postgres.service`만 하면, `Wants=`로 묶인 소비자(`hololive-api`, `hololive-alarm-worker`, `hololive-collector-c`)가 crash나
`HealthOnFailure=stop`으로 자동 재시작할 때 `holo-postgres` start job도 함께 걸려 DB가 다시 기동됩니다.
표식 조건이 막는 것은 명시적 start와 이런 의존 start job입니다(`skipped because of an unmet condition check`).
`Restart=always`의 자동 재시작은 `ConditionPathExists=`를 다시 평가하지 않으므로, 표식이 있어도 fence 중 죽은
소비자는 다시 뜹니다(systemd 257 rootless Quadlet으로 실측). 표식 없이 DB를 멈춰야 하는 정비라면 소비자도 함께 멈춥니다.

## worker profile drop-in과 배포 helper

release별 image·worker profile 고정은 `scripts/deploy/lib/worker-profile-pair.py`가 소유하는
`/etc/containers/systemd/hololive-bot/<unit>.container.d/50-worker-profile.conf` drop-in입니다. 설치 도구는 이 파일을
건드리지 않습니다. drop-in은 `Image=sha256:<id>`, pair label, release 디렉터리의 읽기 전용 mount와
`STACK_WORKER_PROFILE_FILE`을 바꾸고, 기본 unit의 원본 mirror mount는 그대로 둡니다. 서비스 이름은 `hololive-api`,
`hololive-alarm-worker`, `collector`(unit `hololive-collector-c`)이며 이 서비스들은 `podman`으로 고정됩니다.

### 배포 helper

API·worker image 교체는 배포 트리 사본의 `scripts/deploy/quadlet-redeploy-service.sh`로 root로 실행합니다.
다른 위치의 사본은 거부합니다. collector c(와 iris-seoul b)는 `po-central-cutover.sh <c|b>`가 소유합니다.

```bash
helper=/opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh
sudo "$helper" hololive-api sha256:<api-id>
# worker의 migration은 같은 revision의 API image로 실행한다(db-migrate는 API image에만 있다)
sudo "$helper" hololive-alarm-worker sha256:<worker-id> sha256:<api-id>
```

image 인자는 build host의 identity(`sha256:` + `podman image inspect --format '{{.Id}}'`)나 적재한 tag입니다.
helper는 다음 순서로 진행하며 어느 단계도 재시도하지 않습니다.

1. preflight: 배포 트리 revision(`sync-opt-current.sh`가 남긴 `.source-revision`)과 image의
   `org.opencontainers.image.revision`·title·`linux/<host arch>`, primary fence 표식 부재, unit 실행 중,
   host `compose.env` 기준 PostgreSQL 연결 예산(`postgres_capacity_assert_target`)을 확인합니다.
2. 실행 중 container의 이전 pair를 `artifacts/worker-profile/releases/<service>-<UTC>-<suffix>/previous`에 보존합니다
   (`capture`. drop-in이 없는 첫 도입은 `prepare`가 실행 중 image를 먼저 고정합니다). rollback drop-in을 만들고 이전
   image의 `--check-worker-profile`을 확인합니다.
3. candidate pair, `--check-worker-profile`, `verify-source`를 확인합니다.
4. 활성화 전에 `hololive-db-migrate.container.d/50-migrator-image.conf`(`Image=sha256:<API id>`)로 migrator를 고정하고
   `systemctl start hololive-db-migrate.service`를 실행합니다. fence 조건으로 건너뛴 oneshot도 start는 0으로 끝나므로 start
   전후 모두 fence 표식이 없어야 합니다. 실패하면 활성화하지 않고 `[OUTCOME] not_activated`로 끝납니다.
5. candidate drop-in을 활성화하고 `daemon-reload`, `NRestarts` 기준 기록, `systemctl restart`(`Notify=healthy`가
   health gate) 뒤 `verify-live`와 `NRestarts=0`(명시적 restart가 0으로 되돌림)을 확인합니다.
6. 활성화 뒤 실패하면 자동으로 되돌리지 않고 `[OUTCOME] failed_after_activation`으로
   끝납니다. candidate pair와 release 디렉터리는 그대로 둡니다. 이번 배포가 migration을 적용했을 수 있으므로, 이전 pair 복원은
   스키마·admission 호환성 승인 뒤 `scripts/deploy/quadlet-rollback-service.sh <release> --check`와 `--apply`로 합니다.
   migration과 schema는 어느 경우에도 되돌리지 않습니다.

`previous-pair`에서는 서로 다른 두 가지를 확인합니다.

- `capture`·`prepare`는 **실행 중인 writer**의 불변 ID로 로드된 프로필 해시를 확인합니다. 이전 이미지로 네트워크 없는 읽기 전용 컨테이너를 만들고, `podman init` 뒤 실제 UID/GID 매핑·보조 그룹·SELinux MCS·환경과 bind inode를 대조한 후에만 `--check-worker-profile`을 실행합니다. 프로필만 이번에 보관할 바이트로 바꿉니다. 환경값은 호스트 명령행이나 env 파일에 기록하지 않습니다.
- 이어지는 `check`는 **보존한 기본 unit으로도** 그 이미지와 프로필이 시작 가능한지 검사합니다. 이 검사가 첫 검사를 대체하지 않습니다. 원 writer의 정지·재시작·이름 교체, 바뀐 bind, 접근 거부는 새 pair로 게시하지 않습니다. 정지된 writer를 검사 목적으로 켜지 말고 이미 보존한 release로만 재개합니다.

kapu의 SELinux Enforcing 실환경 회귀: `sudo python3 -I -B scripts/deploy/lib/worker-profile-capture_podman_test.py <로컬-host-arch-Alpine-이미지>`. rootful Podman에 이미 있는 이미지를 사용하며 운영 설정·비밀·서비스는 사용하지 않습니다.

### Iris 소비자 동시 전환

인증 계약처럼 API·alarm-worker와 외부 Iris를 같은 창에서 바꿔야 하면 두 소비자를 먼저 보존·정지합니다.
단일 서비스 helper를 정지된 unit에 억지로 적용하거나 API의 `Wants`가 구형 worker를 기동하게 두지 않습니다.

```bash
helper=/opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh
sudo "$helper" --quiesce sha256:<api-id> sha256:<worker-id>
# quiesced 결과의 두 release 경로를 보존하고, 승인된 다른 소비자·Iris 전환을 완료한다.
sudo "$helper" --resume '<api-release-dir>' '<alarm-release-dir>'
```

- `--quiesce`는 실행 중 이전 pair와 후보를 모두 검증·보존한 뒤 두 unit을 함께 정지하고 영속 mask합니다.
  호스트 재부팅 뒤에도 명시적 재개 전에는 구형 writer를 기동하지 않습니다. `[OUTCOME] quiesced`는 writer
  정지만 증명하며 migration·후보 활성화·배포 완료를 뜻하지 않습니다.
- 중앙 두 writer의 소유 표식 `artifacts/worker-profile/releases/.central-writer-hold`는 해당 API release의 `captured.invocation`과 같은 inode입니다. 부분 정지라도 이 표식이나 기동 차단 파일이 남아 있으면 단일 배포와 단일 `rollback --apply`는 거부합니다. 같은 내용을 복사하거나 표식을 지워 재개하지 않습니다.
- `--resume`은 두 release가 같은 정지 작업에서 나왔고 그 작업이 만든 영속 mask가 유지되는지 확인합니다. 설치된 후보의 revision·image·프로필·기본 unit·실효 endpoint가 일치해야 합니다. 두 후보 drop-in을 게시하더라도 API는 worker가 수용될 때까지 차단합니다.
- 완료는 exit 0과 두 서비스의 `[OUTCOME] deployed`, 실제 image/profile·health·`NRestarts=0`으로 확인합니다.
  실행 중 pair를 잃은 뒤 과거 파일 이름만으로 복구점을 만들지 않습니다.
- 준비 뒤 drift·부분 게시·실패는 성공으로 바꾸지 않습니다. 기동 전 실패는 hold를 유지하며, 부분 mask나
  기동 후 실패는 실제 unit 상태를 먼저 확인합니다. 자동 unmask·rollback은 없습니다. 원인을 해결한 명시적
  재개도 두 unit의 hold와 같은 보존 pair를 요구하며, 다른 pair나 배포 revision은 거부합니다.


### 컷오버 직후 첫 고정

컷오버로 처음 뜬 app container는 drop-in이 없어 `Image=${..._IMAGE}`(compose.env 값이나 `:prod` tag)를 따릅니다.
그대로 두면 tag를 옮긴 적재 뒤의 crash·unhealthy 재시작이 migration 전에 새 image로 뜨고, 되돌릴 이전 pair도 남지
않습니다. 세 app unit이 healthy가 된 직후 실행 중 container 기준으로 `prepare`해 첫 drop-in을 만듭니다. 실행 중인
image를 그대로 고정하므로 재시작하지 않습니다.

```bash
root=/opt/hololive-bot/compose/current
pair="$root/scripts/deploy/lib/worker-profile-pair.py"
release="$root/artifacts/worker-profile/releases/cutover-$(date -u +%Y%m%dT%H%M%SZ)"
sudo python3 "$pair" prepare hololive-api "$release/hololive-api/previous" hololive-api "$root"
sudo python3 "$pair" prepare hololive-alarm-worker "$release/hololive-alarm-worker/previous" hololive-alarm-worker "$root"
sudo python3 "$pair" prepare collector-c "$release/collector-c/previous" hololive-collector-c "$root"
sudo systemctl daemon-reload
```

### 개별 단계

helper와 `po-central-remote.sh`가 쓰는 pair 도구 단계입니다. 복구·검사에서 직접 쓸 때 참고합니다.

```bash
root=/opt/hololive-bot/compose/current
pair="$root/artifacts/worker-profile/releases/<release>/<service>/candidate"
# 후보: 기본 unit을 stdin으로, 배포할 image identity를 인자로 준다
sudo python3 scripts/deploy/lib/worker-profile-pair.py candidate <service> "$pair" sha256:<id> \
  < deploy/quadlet/hololive-bot/<unit>.container
sudo python3 scripts/deploy/lib/worker-profile-pair.py verify-source <service> "$pair"
# 활성화: drop-in 설치 + active pointer. 재시작은 승인된 절차에서 한다
sudo python3 scripts/deploy/lib/worker-profile-pair.py activate <service> "$pair" "$root"
sudo systemctl daemon-reload && sudo systemctl restart <unit>.service
sudo python3 scripts/deploy/lib/worker-profile-pair.py verify-live <service> "$pair" <container>
```

`prepare`·`capture`(실행 중인 이전 프로세스의 profile 고정)와 `rollback <service> <pair> [output]`도 같은 drop-in을
만듭니다. `hololive-api` rollback drop-in은 이전 image의 `APP_VERSION`을 `Environment=`로 고정합니다(Podman은
`--env`를 `--env-file`보다 우선합니다).

## 이미지

unit은 `Image=${..._IMAGE}`와 `Pull=never`이고 `[Service]` 기본값은 `localhost/<name>:<tag>`입니다. 실제 identity
(`sha256:` + `podman image inspect --format '{{.Id}}'`)는 image마다 다음이 소유합니다. 빌드는
`./build-all.sh --build-only --no-bump`(`podman build`, 다른 아키텍처는 `HOLOLIVE_BUILD_PLATFORM=linux/arm64`와 등록된
binfmt가 필요)입니다.

| image | identity를 정하는 곳 |
| --- | --- |
| `hololive-api`, `hololive-alarm-worker`, collector c | `50-worker-profile.conf`의 `Image=sha256:<id>`. compose.env의 `HOLOLIVE_*_IMAGE`와 `:prod`는 drop-in이 없을 때(컷오버 직후 첫 고정 전)만 쓰입니다 |
| `hololive-db-migrate` | 배포 helper가 쓰는 `50-migrator-image.conf`. 수동 `systemctl start hololive-db-migrate.service`도 마지막 배포의 migrator를 씁니다 |
| `POSTGRES_IMAGE`, `VALKEY_IMAGE`, `NGINX_IMAGE` | host `compose.env`의 Podman identity(`sha256:<id>`) |
| PO issuer(`hololive-youtube-po-c`) | unit 기본 `localhost/hololive-youtube-po-sandbox:prod` tag |

- infra image(`POSTGRES_IMAGE`, `VALKEY_IMAGE`, `NGINX_IMAGE`)는 compose.env에 `sha256:` + `podman image inspect --format '{{.Id}}'`로
  둡니다. `name:tag@sha256:<digest>` 형식은 Podman store에서 찾지 못해 `Pull=never` unit이 5초 간격 재시작만
  반복합니다. 값은 stack-secrets master(`hosts/hololive-seoul/hololive-bot/compose.env`)에 반영해 `sync-host.sh` 미러가
  되돌리지 않게 합니다. 교체는 load → build host identity 대조 → compose.env 갱신 → 승인된 restart 순서입니다.
- PO issuer는 `:prod` tag를 따르며 `:prod`는 `scripts/deploy/lib/po-central-remote.sh`만 옮깁니다(`:candidate`로 적재해
  검증한 뒤 cutover에서 `:prod`로 옮기고, 실패하면 `:rollback-*`에서 되돌립니다). compose.env에
  `HOLOLIVE_YOUTUBE_PO_IMAGE`를 두지 않습니다. 빈 값도 unit 기본값을 덮어 기동이 실패하고, sha256으로 고정하면 이
  스크립트의 tag 기반 교체·복원과 어긋납니다.
