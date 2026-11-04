# 중앙 Quadlet unit

중앙 호스트(hololive-seoul, `deploy/topology/hosts.conf`의 `central`)는 rootful Podman과 systemd
Quadlet으로 운영합니다. Seoul AP(iris-seoul)는 Docker로 남으며 `deploy/compose/`의 AP compose만 씁니다.

## 구성

| 경로 | 내용 |
| --- | --- |
| `hololive-bot/` | 중앙 운영 unit. 호스트의 `/etc/containers/systemd/hololive-bot/`에 설치합니다 |
| `optional/x-spaces/` | `HOLOLIVE_X_SPACES_ENABLED=1`일 때만 설치하는 drop-in(`40-x-spaces.conf`) |
| `hololive-bot-standby/` | 비활성 PostgreSQL 대기 서버 참고 unit. 중앙 설치 대상이 아니며 `[Install]`이 없습니다 |

`optional/`은 설치 트리 밖에 둡니다. Quadlet은 같은 검색 루트 아래 형제 디렉터리의
`<unit>.container.d/` drop-in도 적용하므로, 선택 drop-in을 `hololive-bot/` 옆에 두면 항상 적용됩니다.

| unit | 컨테이너 | 비고 |
| --- | --- | --- |
| `holo-postgres.container` | `holo-postgres` | 권위 primary. `admin-dashboard-ingress-firewall.service`를 Requires |
| `valkey-cache.container` | `valkey-cache` | Valkey HA hololive 그룹 노드. 역할은 `/data/valkey.conf`가 소유 |
| `hololive-db-migrate.container` | `hololive-db-migrate` | one-shot. `[Install]` 없음 |
| `hololive-alarm-worker.container` | `hololive-alarm-worker` | 알림 dispatcher(단일 인스턴스) |
| `hololive-api.container` | `hololive-api` | bot/admin/llm plane |
| `hololive-youtube-po-c.container` | `hololive-youtube-po-c` | collector c의 격리 PO issuer |
| `hololive-youtube-collector-c.container` | `hololive-youtube-collector-c` | collector fleet c. network alias `youtube-collector` |
| `admin-dashboard-ingress.container` | `admin-dashboard-ingress` | host network shortlink ingress |
| `hololive-net.network` | `hololive-bot_hololive-net` | 172.23.0.0/16, interface `pm-holo` |
| `observability-traces.network` | `observability-traces` | 172.18.0.0/16, interface `pm-otraces` |

## 설치

배포할 commit의 clean checkout에서 root로 실행합니다. `/opt/hololive-bot/compose/current`는 `git archive` 결과라
git 저장소가 아니므로 거기서 실행하면 `repo git tree missing`으로 멈춥니다.

```bash
sudo ./scripts/deploy/sync-opt-current.sh
```

설치 도구는 다음을 확인한 뒤에만 unit을 설치합니다.

- hostname이 `kapu`이면 `HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1` 없이 거부합니다(두 번째 dispatcher 방지).
- `compose.env`에 `HOLOLIVE_VALKEY_HA_ENABLED=1`과 비어 있지 않은 `VALKEY_SENTINEL_ADDRS`가 있어야 합니다.
- `HOLOLIVE_X_SPACES_ENABLED`는 `0` 또는 `1`이며, 그 값에 따라 x-spaces drop-in을 설치하거나 지웁니다.
- `compose.env`의 PostgreSQL pool 값이 연결 예산(`scripts/deploy/lib/postgres-capacity.sh`)을 넘으면 거부합니다.
- 설치 전에 설치 뒤와 같은 구성(staged unit, 켜진 x-spaces drop-in, 설치 도구가 건드리지 않는 기존 drop-in)이
  `quadlet -dryrun`을 통과해야 합니다. 실패하면 `/opt`·prestart·unit을 바꾸지 않습니다.
- 설치 뒤 실행 트리가 root 소유여야 합니다.

배포 트리에는 archive한 commit을 `.source-revision`으로 남깁니다. 배포 helper가 image revision을 이 값과 대조합니다.

방화벽 규칙(`scripts/systemd/admin-dashboard-ingress.nft`)이 바뀐 배포에서는 설치 뒤
`sudo systemctl reload admin-dashboard-ingress-firewall.service`로 적용합니다(아래 '기동·정지·의존').

prestart helper는 `/usr/local/libexec/hololive-bot/quadlet-prestart`(+ `lib/`)로 설치됩니다.

## 호스트 값과 비밀

- 모든 unit이 `[Service] EnvironmentFile=/etc/stack-secrets/hololive-bot/compose.env`를 읽습니다.
  `[Service] Environment=`는 compose의 `${VAR:-기본값}` 기본값이며 compose.env가 우선합니다.
- compose.env는 `KEY=VALUE` 줄만 받습니다. 잘못된 key, 중복 key, 값의 앞뒤 공백은 prestart가 키 이름만 보고하고
  거부합니다. `KEY=` 빈 줄은 unit 기본값을 빈 값으로 덮으므로 기본값을 쓰려면 줄을 지웁니다.
- 비밀번호·API key는 `podman run` 인자에 남지 않도록 prestart가
  `/run/hololive-bot/quadlet-env/<container>.env`(디렉터리 0700, 파일 0600)에 써서 `EnvironmentFile=`로 넘깁니다.
  서비스별 env 파일(`bot.env`, `alarm-worker.env`, `youtube-collector.env`) 다음 순서라 compose의
  `environment` > `env_file` 우선순위를 유지합니다.
- compose에서 `:?`였던 필수 값(`VALKEY_SENTINEL_ADDRS`, `HOLOLIVE_OTLP_GRPC_ENDPOINT`, `LIVE_LOGS_PATH`,
  `POSTGRES_PORT_BIND_IP`, `VALKEY_BIND_IP`, `HOLOLIVE_API_PORT_BIND_IP`, `HOLOLIVE_ADMIN_API_PORT_BIND_IP`,
  `HOLOLIVE_BOT_PORT_BIND_IP`)은 prestart가 확인합니다. tailnet publish 주소는 `tailscale0`에 붙을 때까지 90초 기다립니다.
- `hololive-api`의 `APP_VERSION`은 배포 트리 `hololive/hololive-api/VERSION`에서 읽습니다.

## 기동·정지·의존

- 상시 unit은 `WantedBy=multi-user.target`이며 `Notify=healthy`라 start job은 healthy가 된 뒤 끝납니다.
- `HealthOnFailure=stop` + `Restart=always`가 자동 복구입니다. unhealthy container는 StopSignal(PostgreSQL `SIGINT`,
  ingress `SIGQUIT`, 나머지 `SIGTERM`)과 `StopTimeout`(api·worker 45초, PostgreSQL 120초)을 지켜 멈춘 뒤 systemd가 다시
  띄웁니다. Docker 시절 deunhealth의 restart와 같은 동작이며, `kill`(SIGKILL)은 배치 전송과 checkpoint를 끊으므로 쓰지
  않습니다. collector c와 PO issuer는 예외(unhealthy만으로 멈추지 않음)입니다.
- `holo-postgres`는 `TimeoutStopSec=150`입니다. `ExecStop`(`podman rm`)이 `StopTimeout=120`을 다 쓰기 전에 systemd
  기본 90초가 끊지 않게 합니다.
- `Tmpfs=`에는 `noexec,nosuid,nodev`를 적습니다. Podman은 옵션을 준 `--tmpfs`에 Docker처럼 이 옵션을 붙이지 않습니다.
- 런타임 peer(PostgreSQL·Valkey·worker)는 `Wants=`+`After=`입니다. `Requires=`는 명시적 stop/restart를
  소비자에게 전파해 primary fence(소비자 유지)와 Sentinel HA를 깨뜨리기 때문입니다.
  보안 전제인 `admin-dashboard-ingress-firewall.service`와 migration의 PostgreSQL 의존만 `Requires=`입니다.
- 방화벽 규칙 갱신은 `sudo systemctl reload admin-dashboard-ingress-firewall.service`로만 합니다. reload는 table
  선언·삭제·재정의를 한 nft transaction으로 적용해 규칙이 빈 순간이 없고, 새 파일이 거부되면 기존 table이 남습니다.
  `restart`·`stop`은 `Requires=`로 묶인 `holo-postgres`·`hololive-api`·`admin-dashboard-ingress`를 함께
  재시작·정지합니다. reload는 전파되지 않습니다(systemd 257 user unit으로 실측).
- `hololive-api`를 시작하면 `hololive-alarm-worker`도 함께 시작됩니다(Wants).
- collector c는 compose.env의 `HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR=1`이면 `ExecCondition`으로 건너뜁니다.

```bash
sudo systemctl status hololive-api.service
sudo systemctl show hololive-api.service -p NRestarts   # 마지막 명시적 restart 뒤 자동 재시작 횟수
sudo journalctl -u hololive-api.service --since -15m
```

API·worker의 image 교체와 migration은 아래 '배포 helper'로만 합니다.

## Primary fence

모든 unit은 `/var/lib/hololive-postgres-fence/fence.intent`·`fenced` 중 하나라도 있으면 시작하지 않습니다
(`ConditionPathExists=!`). `scripts/ops/postgres-primary-fence.sh`는 이 조건이 loaded unit에 있는지 확인하고,
intent를 기록한 뒤 Tailscale Service를 drain하고 `holo-postgres.service`만 멈춥니다.
`postgres-primary-unfence.sh`는 수동 기동한 standby(`holo-postgres`, restart `no`, systemd 라벨 없음,
`hololive-bot_holo-pg-data` volume)를 검증한 뒤 표식을 치우고 `holo-postgres.service`로 넘기며,
다시 standby인지 확인하지 못하면 unit을 멈추고 같은 표식을 되돌립니다.

`holo-postgres.service`만 멈추는 일은 이 fence 절차로만 합니다. 표식 없이 `systemctl stop holo-postgres.service`만
하면, `Wants=`로 묶인 소비자(`hololive-api`, `hololive-alarm-worker`, `hololive-youtube-collector-c`)가 crash나
`HealthOnFailure=stop`으로 자동 재시작할 때 `holo-postgres` start job도 함께 걸려 DB가 다시 기동됩니다.
표식 조건이 막는 것은 명시적 start와 이런 의존 start job입니다(`skipped because of an unmet condition check`).
`Restart=always`의 자동 재시작은 `ConditionPathExists=`를 다시 평가하지 않으므로, 표식이 있어도 fence 중 죽은
소비자는 다시 뜹니다(systemd 257 rootless Quadlet으로 실측). 표식 없이 DB를 멈춰야 하는 정비라면 소비자도 함께 멈춥니다.

## worker profile drop-in과 배포 helper

release별 image·worker profile 고정은 `scripts/deploy/lib/worker-profile-pair.py`가 소유하는
`/etc/containers/systemd/hololive-bot/<unit>.container.d/50-worker-profile.conf` drop-in입니다. 설치 도구는 이 파일을
건드리지 않습니다. drop-in은 `Image=sha256:<id>`, pair label, release 디렉터리의 읽기 전용 mount와
`STACK_WORKER_PROFILE_FILE`을 바꾸고, 기본 unit의 원본 mirror mount는 그대로 둡니다. 서비스 이름은 `hololive-api`,
`hololive-alarm-worker`, `youtube-collector`(unit `hololive-youtube-collector-c`)이며 이 서비스들은 `podman`으로 고정됩니다.

### 배포 helper

API·worker image 교체는 배포 트리 사본의 `scripts/deploy/quadlet-redeploy-service.sh`로 root로 실행합니다.
다른 위치의 사본은 거부합니다. collector c는 `po-central-cutover.sh`가 소유합니다.

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
6. 활성화 뒤 실패하면 Docker 시절 `compose-redeploy-service.sh`처럼 자동으로 되돌리지 않고 `[OUTCOME] failed_after_activation`으로
   끝납니다. candidate pair와 release 디렉터리는 그대로 둡니다. 이번 배포가 migration을 적용했을 수 있으므로, 이전 pair 복원은
   스키마·admission 호환성 승인 뒤 `scripts/deploy/quadlet-rollback-service.sh <release> --check`와 `--apply`로 합니다.
   migration과 schema는 어느 경우에도 되돌리지 않습니다.

### 컷오버 직후 첫 고정

컷오버로 처음 뜬 app container는 drop-in이 없어 `Image=${..._IMAGE}`(compose.env 값이나 `:prod` tag)를 따릅니다.
그대로 두면 tag를 옮긴 적재 뒤의 crash·unhealthy 재시작이 migration 전에 새 image로 뜨고, 되돌릴 이전 pair도 남지
않습니다. 세 app unit이 healthy가 된 직후 실행 중 container 기준으로 `prepare`해 첫 drop-in을 만듭니다. 실행 중인
image를 그대로 고정하므로 재시작하지 않습니다.

Docker에서 넘어온 호스트는 `artifacts/worker-profile/active/<service>.pair`가 Docker 시절 pair를 가리킵니다.
`prepare`는 label 없는 container의 이전 pair를 이 포인터에서 찾으므로, Docker image identity와 달라
`profile pair image identity mismatch`로 멈춥니다(2026-11-04 hololive-seoul). 첫 고정 전에 이 디렉터리를
`active.docker-era-<날짜>`로 옮깁니다. release 디렉터리와 Docker 롤백 자료는 그대로 남습니다.

```bash
root=/opt/hololive-bot/compose/current
pair="$root/scripts/deploy/lib/worker-profile-pair.py"
[[ -d $root/artifacts/worker-profile/active ]] && \
  sudo mv "$root/artifacts/worker-profile/active" "$root/artifacts/worker-profile/active.docker-era-$(date -u +%Y%m%d)"
release="$root/artifacts/worker-profile/releases/cutover-$(date -u +%Y%m%dT%H%M%SZ)"
sudo python3 "$pair" prepare hololive-api "$release/hololive-api/previous" hololive-api "$root"
sudo python3 "$pair" prepare hololive-alarm-worker "$release/hololive-alarm-worker/previous" hololive-alarm-worker "$root"
sudo python3 "$pair" prepare youtube-collector "$release/youtube-collector/previous" hololive-youtube-collector-c "$root"
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
  둡니다. Docker 시절 `name:tag@sha256:<digest>` 값은 Podman store에서 찾지 못해 `Pull=never` unit이 5초 간격 재시작만
  반복합니다. 값은 stack-secrets master(`hosts/hololive-seoul/hololive-bot/compose.env`)에 반영해 `sync-host.sh` 미러가
  되돌리지 않게 합니다. 교체는 load → build host identity 대조 → compose.env 갱신 → 승인된 restart 순서입니다.
- PO issuer는 `:prod` tag를 따르며 `:prod`는 `scripts/deploy/lib/po-central-remote.sh`만 옮깁니다(`:candidate`로 적재해
  검증한 뒤 cutover에서 `:prod`로 옮기고, 실패하면 `:rollback-*`에서 되돌립니다). compose.env에
  `HOLOLIVE_YOUTUBE_PO_IMAGE`를 두지 않습니다. 빈 값도 unit 기본값을 덮어 기동이 실패하고, sha256으로 고정하면 이
  스크립트의 tag 기반 교체·복원과 어긋납니다.
