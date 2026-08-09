# Compose Files

- `docker-compose.prod.yml`: production baseline for the main host.
- `docker-compose.live-compat.yml`: opt-in compatibility overlay for pre-hardening live wiring.
- `docker-compose.main-ap.yml`: main-host `youtube-producer-c` overlay.
- `docker-compose.main-ap.live-compat.yml`: live-compat overlay for `youtube-producer-c`.
- `docker-compose.osaka.yml`: Osaka AP overlay for `youtube-producer-a`.
- `docker-compose.osaka2.yml`: second Osaka AP overlay for `youtube-producer-d`.
- `docker-compose.seoul.yml`: Seoul AP overlay for `youtube-producer-b`.
- `docker-compose.remote-cache.yml`: optional BuildKit registry cache overlay. Export is fixed to
  `mode=min`, so only final-image cache records are published; intermediate build stages and copied
  source trees must not be exported by this operational overlay.

Prefer repository wrappers over raw `docker compose`:

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml config
./scripts/deploy/compose-redeploy-service.sh <service>
```

## Runtime Env Inputs

`scripts/deploy/compose.sh` injects one Compose interpolation env file with `--env-file`.
The host default is:

```text
/etc/stack-secrets/hololive-bot/compose.env
```

Use `COMPOSE_ENV_FILE=<path>` for local tests or transition-period compatibility. The
legacy monolithic `env` file is no longer a production `env_file` default.
AP deploy/rollback wrappers set `COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/ap-compose.env`
so AP hosts do not receive Iris egress tokens in their Compose interpolation file.

Application-only env is scoped per service:

```text
HOLOLIVE_API_ENV_FILE=/etc/stack-secrets/hololive-bot/bot.env
HOLOLIVE_ALARM_WORKER_ENV_FILE=/etc/stack-secrets/hololive-bot/alarm-worker.env
HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE=/etc/stack-secrets/hololive-bot/youtube-producer.env
```

### Central endpoint ownership

AP overlays never hardcode the central address. All three (`docker-compose.seoul.yml`,
`docker-compose.osaka.yml`, `docker-compose.osaka2.yml`) read one key family and fail the
render when it is absent, so a host move cannot leave an AP silently pointed at the
retired address:

```text
HOLOLIVE_CENTRAL_POSTGRES_HOST   required
HOLOLIVE_CENTRAL_CACHE_HOST      required
HOLOLIVE_CENTRAL_POSTGRES_PORT   default 5433
HOLOLIVE_CENTRAL_CACHE_PORT      default 6379
```

`ap-compose.env` owns those values. `CLIPROXY_BASE_URL` comes from the same file through
`docker-compose.prod.yml`; AP overlays must not re-declare it. Host-native APs
(`youtube-producer-a`/`-d`) get the same endpoint from `AP_CENTRAL_HOST` in
`scripts/deploy/ap-host-native-deploy.sh`.

AP overlays use only `youtube-producer.env` for `youtube-producer` instances, so Iris
egress tokens stay out of AP producer containers. Osaka/Seoul AP hosts also use
`ap-compose.env`, which excludes `IRIS_WEBHOOK_TOKEN` and `IRIS_BOT_TOKEN`.
`docker-compose.main-ap.yml` also uses scoped `youtube-producer.env` for
`youtube-producer-c`; it still must not receive Iris egress tokens or the
monolithic Compose env file as an `env_file`.

Deploy this repo-side contract after `tools/sync-host.sh <host> --apply` has mirrored
`compose.env` or `ap-compose.env` plus the per-service env files to the target host.

`scripts/deploy/lib/postgres-capacity.sh`가 production mutation entrypoint의 공통 owner입니다.
`scripts/deploy/compose.sh ... up`, `build-all.sh`, `scripts/deploy/compose-redeploy-service.sh`는
build/migration/up보다 먼저 이 gate를 호출해 `COMPOSE_ENV_FILE`의 PostgreSQL pool override key만 읽고
stack 전체 connection budget을 다시 계산합니다. Target-rendered allocation이 `max_connections=60`에서 최소 5개
reserve를 남기지 않으면 어떤 표준 배포 경로도 진행하지 않습니다. Default policy만 확인할 때는
`scripts/ci/check-postgres-capacity.sh`를, 특정 render 결과를 확인할 때는 세 번째 인자로 해당
Compose env file을 전달합니다. 이 검사는 다른 env 값이나 secret을 출력하지 않습니다.

## PostgreSQL TLS

`holo-postgres` serves TLS with `ssl=on`. The `stack-secrets` master owns the server
certificate and key; the host copy lives at `/etc/stack-secrets/hololive-bot/postgres-tls/`
and mounts read-only at `/run/hololive-bot/postgres-tls/`, outside the client-mounted
`certs/` directory. Reissuance sends `SIGHUP` to `holo-postgres` so PostgreSQL reloads
the server material without a container recreate.

All production PostgreSQL clients use `verify-full` with the CA bundle mounted
at `/run/hololive-bot/certs/postgres-ca.pem`: the five central Go runtimes,
`youtube-producer-c`, `hololive-db-migrate`, Osaka `youtube-producer-a`,
Osaka2 `youtube-producer-d`, and Seoul `youtube-producer-b`. The retired
insecure downgrade ledger stays closed by preserving production paths with
verified TLS and the CA bundle above.

## Requirements

- Docker Compose v2.24.4+ — 오버레이의 `!override` YAML 태그가 이 버전부터 지원된다.
  build `provenance`/`sbom` 속성도 지원해야 한다.
- BuildKit 활성 Docker Engine — Dockerfile들의 `# syntax=docker/dockerfile:1.24.0@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89`
  (cache mount, `COPY --link`, per-Dockerfile `.dockerignore`) 전제.
- production Go build는 `GOWORK=off`로 각 `go.mod`의 stable published external pin만 사용한다.
  로컬 sibling checkout은 image source가 아니다.
- 호스트 호환성 확인: `docker compose -f deploy/compose/docker-compose.prod.yml config` 가
  에러 없이 렌더되는지로 검증한다.
