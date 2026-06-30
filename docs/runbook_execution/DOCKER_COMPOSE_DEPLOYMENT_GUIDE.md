# Hololive Docker Compose Deployment Guide

## 목적

단일 호스트 `docker compose` 기반으로 hololive runtime을 운영하기 위한 기본 절차입니다.

> 운영 기준 (2026-03-07): 기존 k8s/k3s 배포에서 Docker Compose 기준으로 롤백했습니다. 현재 운영에서는 `kubectl`, `kustomize`, `helm` 절차 대신 이 문서를 우선 사용합니다.

대상 서비스:
- `hololive-api` (통합 런타임 — bot plane `30001`, llm plane `30003`, admin plane `30006`)
- `hololive-alarm-worker` (`30007`)
- `youtube-producer` (3-way AP: osaka `30005` / seoul `30015` / main `30025`)
- `holo-postgres` (`5433`)
- `valkey-cache` (`6379`)

## 운영 원칙

- 프로덕션 배포 진입점은 `./build-all.sh --no-bump` 또는 `./scripts/deploy/compose-redeploy-service.sh <service>`입니다.
- 직접 Compose 명령이 필요하면 raw `docker compose` 대신 `./scripts/deploy/compose.sh`를 사용합니다. 이 wrapper는 OpenBao env preflight와 shell shadowing 차단을 먼저 수행합니다.
- OpenBao env 전환 후 운영 호스트의 local `.env` 파일과 shell profile export 잔재는 [OpenBao Env Cleanup Runbook](./OPENBAO_ENV_CLEANUP_RUNBOOK.md) 기준으로 정리합니다.
- 상태/장애 1차 확인은 `./scripts/deploy/compose.sh -f docker-compose.prod.yml ps`, `./scripts/deploy/compose.sh ... logs`, `/health`, `/ready` 기준으로 수행합니다.
- k8s/k3s 시절 절차나 매니페스트가 저장소에 남아 있더라도, 현재 운영 SSOT로 간주하지 않습니다.
- 앱 이미지는 distroless에서 UID/GID `1000:1000`으로 실행하고 `/etc/passwd`의 `app` 사용자와 `USER=app`, `HOME=/tmp`를 함께 제공합니다.
- Compose healthcheck는 Dockerfile의 `HEALTHCHECK --start-period=5s`보다 운영 Compose anchor의 `start_period: 30s`를 우선 적용합니다.
- Compose가 Dockerfile 기본값을 의도적으로 override하는 값이 있습니다. `hololive-api`는 Compose에서 `GOGC=80`, `GOMEMLIMIT=1024MiB`를 적용합니다.
- `docker-proxy`, `deunhealth` 기본 이미지는 `latest` 대신 manifest digest로 고정합니다. 갱신 시 OpenBao env의 `DOCKER_SOCKET_PROXY_IMAGE`, `DEUNHEALTH_IMAGE`를 새 tag/digest로 명시하고 재검증합니다.

앱 bind mount는 UID `1000`이 쓸 수 있어야 합니다. 운영자 group이 `docker`가 아닌 호스트에서는 아래 group 부분을 실제 운영자 group으로 바꿉니다.

```bash
mkdir -p data logs runtime-config
sudo chown -R 1000:docker data logs
sudo find data logs -type d -exec chmod 2770 {} +
sudo find data logs -type f -exec chmod 660 {} +
```

## YouTube producer 런타임

`docker-compose.prod.yml` 기준 현재 YouTube 수집과 photo sync 책임은 `youtube-producer` 서비스가 소유하며, 런타임은 3-way active-active 인스턴스로 실행됩니다 (osaka `youtube-producer-a` `30005`, seoul `youtube-producer-b` `30015`, main `youtube-producer-c` `30025`).

- `youtube-producer` (base 정의, 인스턴스 overlay가 포트·instance id·PhotoSync 참여를 override — `youtube-producer-b`는 PhotoSync 미참여): `YOUTUBE_INGESTION_ENABLED=true`, `PHOTO_SYNC_ENABLED=true`, `YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED=true`
  - YouTube ingestion scheduler
  - YouTube producer scheduler
  - YouTube outbox row production; final send is owned by `alarm-worker`
  - Holodex photo sync
  - `config:update` 구독 (`scraper_proxy` 반영)

운영 라우팅 고정:
- YouTube 커뮤니티/쇼츠 알람은 전체 운영 채널에서 `youtube-producer`가 outbox row를 만들고 `alarm-worker`가 claim/render/final send를 수행합니다.
- compose 기준 rollout key는 `YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED` 하나만 사용하고, canary fallback은 두지 않습니다. 운영 compose에서는 `youtube-producer=true`로 고정합니다.
- `youtube-producer` 실행 권한은 `YOUTUBE_PRODUCER_RUNTIME_ALLOWED`로 한 번 더 제한합니다. base 기본값은 `false`이고 원격 AP overlay(osaka/seoul)와 `main-ap` profile에서만 `true`입니다.

## Remote AP split-host 운영 (osaka, seoul)

> 실제 tailnet 주소/호스트는 private ops evidence 참조.

split-host 구성에서는 producer AP runtime container만 원격 호스트에서 실행하고, shared state/control은 기존 `kapu` (`<tailnet-central>`)에 둡니다. 토폴로지는 3-way active-active입니다.

- Osaka runtime: `youtube-producer-a` (`<osaka-a-host>`, `<tailnet-osaka-a>`, overlay `docker-compose.osaka.yml`, port `30005`)
- Seoul runtime: `youtube-producer-b` (`<tailnet-seoul-b>`, overlay `docker-compose.seoul.yml`, port `30015`)
- main-host runtime: `youtube-producer-c` (`docker-compose.main-ap.yml`, profile `main-ap`, port `30025`)
- 기존 state/control: `holo-postgres` (`<tailnet-central>:5433`), `valkey-cache` (`<tailnet-central>:6379`), CLIProxy (`http://<tailnet-central>:8787/v1`), `hololive-api` (통합 런타임 — bot/llm/admin plane)
- 원격 AP compose env file: OpenBao Agent가 렌더링한 `/run/hololive-bot/ap-compose.env` (`COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env`)
- env 정본은 OpenBao KV입니다. 중앙 Valkey는 Tailscale IP에 publish되므로 password 없이 운영하지 않습니다.
- 중앙 host의 `./scripts/deploy/compose-redeploy-service.sh youtube-producer`는 기본적으로 차단됩니다. 원격 AP overlay 또는 명시적 emergency override 없이 중앙에서 재기동하지 않습니다.

원격 AP 재배포 진입점은 host 파라미터화된 wrapper입니다. rsync/build/recreate/검증 절차는 `docs/current/runbooks/youtube-producer.md`의 Remote AP rollout 섹션을 따릅니다.

```bash
./scripts/deploy/ap-deploy.sh osaka --dry-run
I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh osaka --apply

./scripts/deploy/ap-deploy.sh seoul --dry-run
I_APPROVE_SEOUL_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh seoul --apply
```

수동 service start가 필요하면 원격 AP에서 local infra dependency를 만들지 않도록 `--no-deps`를 붙입니다.

```bash
SSH_OSAKA='ssh -i /home/kapu/gemini/hololive-bot/<ssh-key> -o IdentitiesOnly=yes ubuntu@<osaka-a-host>'
$SSH_OSAKA 'cd ~/hololive-bot && sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.osaka.yml up -d --no-deps --remove-orphans youtube-producer-a'

SSH_SEOUL='ssh -i /home/kapu/gemini/hololive-bot/<ssh-key> -o IdentitiesOnly=yes -o HostKeyAlias=<tailnet-seoul-b> ubuntu@<tailnet-seoul-b>'
$SSH_SEOUL 'cd ~/hololive-bot && sudo -n env COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f docker-compose.prod.yml -f docker-compose.seoul.yml up -d --no-deps --remove-orphans youtube-producer-b'
```

컷오버는 build를 먼저 완료한 뒤 원격 AP를 한 번에 한 호스트씩 교체합니다. `youtube-producer`는 outbox row producer이므로 승인된 active-active guard 없이 여러 호스트에서 동시에 실행하지 않습니다.

원격 AP 호스트에는 source tree를 상시 보관하지 않습니다. 운영 디렉터리에는 `scripts/deploy/ap-rsync-files.txt` 매니페스트가 동기화한 compose/build 파일과 `runtime-config/`, `data/`, `logs/`만 두고 AP compose env는 `/run/hololive-bot/ap-compose.env`, producer app env는 `/run/hololive-bot/youtube-producer.env`에서만 읽습니다. image는 해당 호스트가 동기화된 build context에서 직접 build합니다.

원격 AP의 bind-mounted `data/`, `logs/`는 컨테이너 `app` (UID `1000`)이 쓰고 `ubuntu`가 읽을 수 있어야 합니다. Osaka/Seoul 모두 host 사용자 `ubuntu`는 UID `1001`이라 chown이 필요하고, `ubuntu`는 `docker` group에 속합니다.

```bash
$SSH_OSAKA 'cd ~/hololive-bot && mkdir -p data logs && sudo chown -R 1000:docker data logs && sudo find data logs -type d -exec chmod 2770 {} + && sudo find data logs -type f -exec chmod 660 {} +'
```

## Compose env 파일 계약

중앙 운영 Compose env 파일의 정본은 OpenBao Agent가 렌더링한 `/run/hololive-bot/compose.env`입니다. AP 운영 Compose env 파일은 token-free `/run/hololive-bot/ap-compose.env`입니다. 이 파일들은 Docker Compose `--env-file`과 POSIX shell `source` 양쪽에서 사용할 수 있어야 합니다.

- 한 줄에 하나의 `KEY=VALUE`만 둡니다.
- key는 `A-Z`, `a-z`, 숫자, `_`만 사용하고 숫자로 시작하지 않습니다.
- key 앞뒤 공백, `=` 없는 줄, 중복 key, 제어 문자는 허용하지 않습니다.
- 앞에 `export`를 붙이지 않습니다.
- command substitution을 넣지 않습니다.
- 공백이 있는 값은 quote 또는 escape 처리합니다.
- shell 특수 문자가 들어간 secret은 single-quote 또는 안전한 escape 형태로 렌더링합니다.

비운영 또는 테스트 배포는 local `.env` fallback을 사용하지 않고 명시적으로 env 파일을 지정합니다.

```bash
COMPOSE_ENV_FILE=./.env.local ./scripts/deploy/compose.sh -f docker-compose.prod.yml config --quiet
COMPOSE_ENV_FILE=./.env.local ./build-all.sh --no-bump --build-only --skip-local-ci
```

## 사전 준비

1. OpenBao Agent 렌더링 확인
   ```bash
   sudo find /run/hololive-bot -maxdepth 2 -printf "%M %u %g %s %p\n" | sort
   test -r /run/hololive-bot/compose.env || { echo "env file is not readable by $(id -un)"; exit 1; }
   export_line="$(awk '/^[[:space:]]*export[[:space:]]+/ { print NR; exit }' /run/hololive-bot/compose.env)"
   if [ -n "$export_line" ]; then
     echo "env file must not contain leading export: /run/hololive-bot/compose.env:$export_line"
     exit 1
   fi
   command_sub_line="$(awk '/[`]|[$][(]/ { print NR; exit }' /run/hololive-bot/compose.env)"
   if [ -n "$command_sub_line" ]; then
     echo "env file must not contain command substitution: /run/hololive-bot/compose.env:$command_sub_line"
     exit 1
   fi
   ```
2. 필수 시크릿/접속값은 OpenBao KV `kv/prod/hololive-bot/env`에 있어야 합니다.
   - `DB_PASSWORD`
   - `CACHE_PASSWORD`
     - `valkey-cache`는 `--requirepass`로 실행되며 모든 앱 컨테이너와 Osaka AP가 같은 값을 사용해야 합니다.
     - admin-dashboard Redis URL에도 들어가므로 URL-safe hex 값을 권장합니다.
   - `IRIS_WEBHOOK_TOKEN`
   - `IRIS_BOT_TOKEN`
     - 필요하면 두 값은 동일하게 둘 수 있지만 변수는 분리해서 유지합니다.
   - `HOLODEX_API_KEY_*`
   - 필요 시 `HOLOLIVE_BOT_PORT_BIND_IP`
     - 기본값: `<tailnet-central>` (기존 운영 Tailscale IP)
     - Tailscale/redroid에서 bot webhook(`30001`)에 접근해야 하면 ARM 호스트의 Tailscale IP로 설정
       - 예: `HOLOLIVE_BOT_PORT_BIND_IP=<tailnet-central>`
   - 필요 시 `VALKEY_PORT_BIND_IP`
     - 기본값: `<tailnet-central>` (기존 운영 Tailscale IP)
     - 다른 Tailscale 노드에서 운영 Valkey를 바라봐야 하면 Valkey가 실행 중인 호스트의 Tailscale IP로 설정
       - Docker port publish는 `100.100.1.*` 와일드카드가 아니라 실제 로컬 IP 하나를 사용합니다.
3. Docker Compose 사용 가능 여부 확인
   ```bash
   docker compose version
   ```

## 기본 기동

```bash
./build-all.sh --no-bump
```

또는:

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml up -d --build
```

## 단일 서비스 재배포

```bash
./scripts/deploy/compose-redeploy-service.sh hololive-api
./scripts/deploy/compose-redeploy-service.sh hololive-alarm-worker
COMPOSE_FILE=docker-compose.prod.yml:docker-compose.main-ap.yml COMPOSE_PROFILES=main-ap ./scripts/deploy/compose-redeploy-service.sh youtube-producer-c
```

base `youtube-producer`의 중앙 재배포는 guard로 차단됩니다. 원격 AP(`youtube-producer-a`/`youtube-producer-b`)는 `./scripts/deploy/ap-deploy.sh <host>`로 재배포합니다.

통합 런타임 plane 역할 (`hololive-api` 단일 프로세스):

- bot plane (`30001`) → ingress (`/webhook/iris`, command routing)
- admin plane (`30006`) → 운영/control plane (`/api/holo/*`, `/api/auth/*`, `/oauth/callback`, `/internal/alarm/*`)
- llm plane (`30003`) → LLM scheduler
- `hololive-alarm-worker` (`30007`, 별도 서비스) → alarm scheduler / checker / queue publish and proactive egress

- worktree에서 중앙 재배포할 때 deploy 스크립트는 기본적으로 `/run/hololive-bot/compose.env`를 사용합니다. AP wrapper는 `/run/hololive-bot/ap-compose.env`를 명시합니다. 비운영 또는 테스트 배포는 `COMPOSE_ENV_FILE`을 명시해야 합니다.

## 상태 확인

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.main-ap.yml exec -T youtube-producer-c ./bin/healthcheck https://127.0.0.1:30025/health
```

Remote AP split-host 상태 확인:

```bash
./scripts/logs/ap-status.sh osaka
./scripts/logs/ap-status.sh seoul
SINCE=15m TAIL=600 PATTERN='ingestion_lease|outbox|ERR|WRN' ./scripts/logs/ap-logs.sh osaka youtube-producer | tail -n 120
SINCE=15m TAIL=600 PATTERN='ingestion_lease|outbox|ERR|WRN' ./scripts/logs/ap-logs.sh seoul youtube-producer | tail -n 120
docker logs --since 15m hololive-youtube-producer-c 2>&1 | grep -E "photo|runtime|ingestion_lease|ERR|WRN" | tail -n 120
```

Tailscale/redroid 연동이 필요한 경우에는 배포 전에 다음을 함께 확인하세요.

```bash
# OpenBao env key
HOLOLIVE_BOT_PORT_BIND_IP=<tailnet-central>

# 재배포 후 H3 health 확인 (내부 probe는 H3 전용)
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
```

- `HOLOLIVE_BOT_PORT_BIND_IP`를 설정하지 않으면 `hololive-api`(bot plane)는 기존 운영 Tailscale IP인 `<tailnet-central>:30001`에도 publish 됩니다.
- `VALKEY_PORT_BIND_IP`를 설정하지 않으면 `valkey-cache`는 기존 운영 Tailscale IP인 `<tailnet-central>:6379`에도 publish 됩니다. 이 포트는 `CACHE_PASSWORD` 인증을 전제로 합니다.
- redroid/Iris 인바운드 webhook에 필요한 포트는 `30001`입니다. webhook ingress는 외부 HTTP/H3 경계(`iris-boundary` 계약)로, 내부 H3 전용 health probe와는 별개 transport입니다.
- `30081`, `30082`는 외부 health dependency 확인용이며 `hololive-api`(bot plane) webhook 자체와는 별도입니다.

### Iris → Bot webhook 계약

redroid/Iris가 `hololive-api` bot plane에 전달하는 인바운드 경로는 아래 기준을 사용합니다. 이 webhook ingress는 외부 HTTP/H3 경계(`iris-boundary` 계약)로, 내부 H3 전용 health probe와는 의도적으로 다른 transport입니다.

- URL: `http://<HOLOLIVE_BOT_PORT_BIND_IP>:30001/webhook/iris`
- Method: `POST`
- Header:
  - `X-Iris-Token: $IRIS_WEBHOOK_TOKEN`
  - `X-Iris-Message-Id: <unique-message-id>` (dedup용, 권장 아님이 아니라 사실상 필수)
- JSON body:

```json
{
  "text": "!도움",
  "room": "123456789",
  "sender": "tester",
  "userId": "user-1",
  "threadId": "thread-1"
}
```

메모:
- 경로는 `/webhook`이 아니라 `/webhook/iris`입니다.
- `threadId`는 현재 인바운드 webhook 스키마에 포함됩니다.
- `X-Iris-Message-Id`가 비어 있으면 bot-side dedup이 동작하지 않습니다.

## 로그 확인

기본 정책은 **애플리케이션 stdout/stderr를 SSOT**로 두고 `./scripts/deploy/compose.sh ... logs`를 직접 조회하는 것입니다.

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f hololive-api
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f hololive-alarm-worker
docker logs -f hololive-youtube-producer-c
```

보조 스크립트:

```bash
./scripts/logs/logs.sh query hololive-api --since 1h --limit 1000
./scripts/logs/logs.sh tail hololive-api --since 30m
./scripts/logs/logs.sh backfill hololive-api --since 24h
./scripts/logs/logs.sh stream start
./scripts/logs/logs.sh prune
```

- compose 런타임에서는 `LOG_DIR=/app/logs`로 설정해 host `./logs/hololive-api.log`, `./logs/alarm-worker.log`, `./logs/youtube-producer-*.log`에 파일 미러링합니다.
- 앱 파일 로그 로테이션 기본값은 `5MB`, `5 backups`, `30일`, `gzip 압축`입니다.
- 압축 백업은 `./logs/archive/*.gz`로 이동해 보관합니다.
- Docker Compose `json-file` 드라이버 로테이션 기본값은 짧은 stdout/stderr 안전 버퍼용 `5MB`, `3 files`입니다.
- 기본 운영 경로는 `logs/*.log`와 `logs/archive/*.gz`입니다.
- Osaka split-host에서도 같은 앱 파일 로그 로테이션 정책을 사용합니다. 별도 일일 log rollup/truncate를 사용하지 않으며, `hololive-osaka-log-rollup.timer`는 masked 상태가 정상입니다.
- `logs/mirror/*.log`는 `ENABLE_LOG_MIRROR=1`일 때만 생성되는 선택적 로컬 미러링이며 운영 SSOT가 아닙니다.
- `logs/backfill/*.log`, `logs/canary/`, `logs/cron/`, `logs/runtime/pids/`는 `ENABLE_LOG_AUX_FILES=1`일 때만 사용하는 보조 운영 산출물입니다.
- 보조 로그 정리는 `./scripts/logs/logs.sh prune` 기준으로 수행합니다.
- 운영 판단의 기준은 `./scripts/deploy/compose.sh ... logs`이며, 별도 log aggregation 전제를 두지 않습니다.

## DB migration

초기화/마이그레이션은 `hololive-db-migrate`가 담당합니다.

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml up --build hololive-db-migrate
```

## 관련 런북

- `docs/runbook_execution/YOUTUBE_PRODUCER_RUNBOOK.md`
- `hololive/hololive-kakao-bot-go/docs/STREAM_INGESTER_RUNBOOK.md`

## 정지 / 재기동

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml stop
./scripts/deploy/compose.sh -f docker-compose.prod.yml start
```

## 완전 종료

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml down
```
