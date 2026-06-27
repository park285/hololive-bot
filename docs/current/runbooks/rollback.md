# Runbook: rollback

## Role

Docker Compose runtime rollback과 contract/document rollback 판단 기준입니다.

## Before Rollback

- Identify the changed runtime, config, contract, or document gate.
- Preserve relevant logs and DLQ samples.
- Check whether rollback would break a newer provider/consumer contract.

## Runtime Rollback

이미지는 registry tag가 아니라 호스트 로컬에서 빌드되는 `<service>:prod` single-tag입니다(`hololive-api:prod`, `hololive-alarm-worker:prod`, `hololive-youtube-producer:prod`). redeploy가 매번 같은 tag를 덮어쓰므로 "이전 tag로 되돌리기"는 불가능하고, code rollback의 기본 경로는 **이전 git ref를 checkout한 뒤 redeploy(rebuild)** 하는 것입니다. 예외적으로 5→3 cutover는 구 per-runtime 이미지가 호스트 로컬에 보존되어 있어 rebuild 없이 재생성할 수 있습니다 — 아래 전용 playbook을 따릅니다.

```bash
./scripts/deploy/compose-redeploy-service.sh <service>
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps <service>
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 <service>
```

Runtime service names:

- `hololive-api`
- `hololive-alarm-worker`
- `youtube-producer`

## 5→3 Cutover Emergency Rollback (hololive-api → 구 bot/admin/llm)

통합 `hololive-api`(bot/admin/llm 단일 프로세스) 컷오버 직후 crash-loop / unhealthy / 기능 회귀가 발생해 구 3-runtime로 되돌려야 할 때의 즉시 실행 절차입니다. 이것은 **단순 재시작이 아니라 재생성**입니다 — `compose-redeploy-service.sh hololive-api`의 `removed_runtime_cleanup_before_cutover`가 구 `hololive-kakao-bot-go`/`hololive-admin-api`/`hololive-llm-scheduler` 컨테이너를 `rm -f` 했으므로 구 컨테이너 자체가 존재하지 않습니다. 구 compose 정의 + 보존된 구 이미지로 다시 만들어야 합니다.

### Confirmed facts (rollback이 안전한 근거)

- **DB schema 호환**: 통합 PR은 신규 migration을 추가하지 않았습니다(`a04f3c54`는 코드 경로 이동만). 따라서 현재 schema == 컷오버 이전 schema이며, 구 bot/admin/llm이 그대로 붙습니다. down-migration 불필요.
- **구 이미지 로컬 보존**: `hololive-kakao-bot-go:prod`, `hololive-admin-api:prod`, `hololive-llm-scheduler:prod`.
- **데이터 보존**: named volume(`holo-pg-data`, `valkey-cache-data`)은 컨테이너 재생성과 무관하게 보존됩니다.

### Step 0 — preconditions

```bash
docker images | grep -E 'hololive-kakao-bot-go|hololive-admin-api|hololive-llm-scheduler'
test -r /run/hololive-bot/compose.env && echo "compose.env OK"
```

- 구 이미지가 보이지 않으면 → 맨 아래 "구 이미지 재build (이미지 부재 시)" 절로. 현 main에서는 rebuild 불가합니다.
- git writer 단일성(meta-repo AGENTS.md) 확인 후 진행합니다.

### Step 1 — 구 compose 정의 export (`35dcf0f8^`)

relative bind mount(`../../logs`, `../../data`, `../../runtime-config`)가 repo root로 풀리도록 **현재 repo의 `deploy/compose/` 안에** export합니다. 별도 worktree/경로에 풀면 `../../logs`가 엉뚱한 디렉터리를 가리켜 빈 logs/data를 마운트하므로 반드시 in-place로 둡니다.

```bash
# from repo root
git show 35dcf0f8^:deploy/compose/docker-compose.prod.yml        > deploy/compose/rollback.prod.yml
git show 35dcf0f8^:deploy/compose/docker-compose.live-compat.yml > deploy/compose/rollback.live-compat.yml
```

live-compat overlay가 필요한 이유: 구 bot의 `:30001`을 tailnet IP(`100.100.1.3`)에 binding해야 Iris(`100.100.1.5`) webhook ingress가 도달합니다. prod overlay만 쓰면 `127.0.0.1`에만 binding되어 봇 ingress가 끊깁니다.

### Step 2 — hololive-api 제거 (포트/alias 회수)

구 3개와 `hololive-api`는 같은 포트(30001/30003/30006)와 같은 network를 공유하므로, 먼저 통합 컨테이너를 내려 포트·network alias를 비웁니다.

```bash
docker stop hololive-api && docker rm -f hololive-api
```

### Step 3 — 구 bot/admin/llm 재생성 (deps 보존)

postgres/valkey/alarm-worker는 running 상태를 유지해야 하므로 `--no-deps`로 dependency 재생성을 막고, `--no-build`로 보존된 구 이미지를 그대로 사용합니다. **repo root에서** 실행합니다.

```bash
sudo -n docker compose --env-file /run/hololive-bot/compose.env \
  -f deploy/compose/rollback.prod.yml -f deploy/compose/rollback.live-compat.yml \
  up -d --no-build --no-deps hololive-bot hololive-admin-api llm-scheduler
```

복구되는 계약(구 정의에서 확인됨):

- **network alias** (hololive-net): `hololive-bot`, `hololive-admin-api`, `llm-scheduler`. 내부 호출(bot/admin → `https://llm-scheduler:30003`)이 이 alias에 의존합니다.
- **ports**: bot `100.100.1.3:30001`(+/udp, live-compat) / admin `127.0.0.1:30006` / llm `127.0.0.1:30003`.
- **env_file**: bot만 `/run/hololive-bot/bot.env`. admin/llm은 env_file 없이 compose inline env(컷오버와 동일 시크릿 소스)를 씁니다.
- **cert mounts**: `/run/hololive-bot/certs/{hololive-h3.crt,hololive-h3.key,iris-ca.pem,postgres-ca.pem}` — 절대경로라 변동 없음.
- **docker-proxy 의존**: bot은 `DOCKER_HOST=tcp://docker-proxy:2375` + `docker-proxy-net`을 씁니다. docker-proxy가 내려가 있으면 컨테이너-관리 기능만 degrade되고 core bot은 기동됩니다. 필요 시 `... up -d --no-deps docker-proxy`를 추가합니다.

### Step 4 — 검증 (normal / failure / recovery)

```bash
docker ps --filter name=hololive-kakao-bot-go --filter name=hololive-admin-api --filter name=hololive-llm-scheduler
docker inspect hololive-kakao-bot-go --format '{{.State.Health.Status}} {{.RestartCount}}'
docker logs --tail=200 hololive-kakao-bot-go
```

- **bot 실응답(live 검증 필수)**: 실제 KakaoTalk 메시지로 webhook→reply 왕복을 확인합니다. 코드/로그 레벨만으로는 불충분하다는 것이 이번 컷오버 교훈입니다.
- **failure path**: 구 bot이 다시 crash하면 logs에서 cert/env/DB 원인을 분리하고, 미해결 시 forward-fix(`hololive-api` 재배포)로 전환합니다.

### Step 5 — cleanup

```bash
rm -f deploy/compose/rollback.prod.yml deploy/compose/rollback.live-compat.yml
```

export한 임시 compose 파일은 운영 산출물이므로 commit하지 않습니다.

### 구 이미지 재build (이미지 부재 시)

통합이 구 모듈 디렉터리(`hololive/hololive-kakao-bot-go`, `hololive/hololive-admin-api`, `hololive/hololive-llm-sched`)와 init-db/migration mount 소스를 **현 main에서 삭제**했습니다. 따라서 현 main 트리에서는 구 이미지를 rebuild할 수 없습니다. rebuild가 필요하면 `35dcf0f8^`(또는 통합 머지 이전 ref)를 별도 checkout/worktree로 풀어 그 트리에서 `docker build`해야 합니다. 단 컨테이너 실행(Step 3)은 relative-mount 때문에 여전히 현재 working tree의 `deploy/compose`에서 수행합니다.

## Contract Rollback

- HTTP contract rollback must preserve route constants expected by deployed consumers.
- Queue envelope rollback must keep consumers able to read already-enqueued messages.
- Pub/Sub rollback must tolerate missed messages and trigger startup refresh where needed.
- Iris boundary rollback must verify cert/token/transport compatibility.

## Post-Rollback Smoke Tests

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/smoke/smoke-compose-config.sh
./scripts/smoke/smoke-runtime-health.sh
```

## Related documents

- `release.md`
- `../CONTRACT_MAP.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
- `../../runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
