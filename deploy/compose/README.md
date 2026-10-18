# Compose Files

- `docker-compose.prod.yml`: production baseline for the main host.
- `docker-compose.live-compat.yml`: live wiring overlay the main host always applies with the production
  baseline (ports, volumes, and the main-host `youtube-collector-c`, which is `youtube-collector` in
  `docker-compose.prod.yml`).
- `docker-compose.osaka.yml`: Osaka AP overlay for `youtube-collector-a`.
- `docker-compose.osaka2.yml`: second Osaka AP overlay for `youtube-collector-d`.
- `docker-compose.seoul.yml`: Seoul AP overlay for `youtube-collector-b`.
- `docker-compose.remote-cache.yml`: optional BuildKit registry cache overlay. Export is fixed to
  `mode=min`, so only final-image cache records are published; intermediate build stages and copied
  source trees must not be exported by this operational overlay.

Prefer repository wrappers over raw `docker compose`:

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml config
./scripts/deploy/compose-redeploy-service.sh <service>
```

## Owned ingress image

`admin-dashboard-ingress` uses the local-only `iris-stack-nginx:1.31.7-zlib1.3.2-r1`
image (`pull_policy: never`). Its only build context is `deploy/images/nginx`; the
immutable official Nginx base retains its entrypoint, configuration and users,
with Alpine 3.24 `zlib=1.3.2-r1` applied for CVE-2026-85091. The PostgreSQL runtime
applies the same zlib fix alongside its existing `nghttp2-libs=1.70.0-r0` pin.

Build the ingress explicitly with the production and live-compat Compose files
and the `admin-dashboard-ingress` build target; `build-all.sh` only selects the
base production file. For isolated local builds without runtime env inputs:

```bash
docker buildx build --platform linux/arm64 --build-arg REVISION=unknown \
  --provenance=false --sbom=false --load \
  --tag iris-stack-nginx:1.31.7-zlib1.3.2-r1 deploy/images/nginx
```

Use the full committed image-source revision instead of `unknown` only after the
source is committed. Other stack ingress consumers must load this same owned
image rather than maintain a second recipe or pull the unpatched official tag.
The existing ingress behavior checks build an isolated temporary tag from this
context; `NGINX_TEST_PLATFORM` selects the architecture (default `linux/amd64`).

`valkey-cache` and `docker-proxy` also use local-only owned images:
`iris-stack-valkey:9.1.2-zlib1.3.2-r1` applies the same zlib fix to the pinned official
Valkey base, while `iris-stack-socket-proxy:1.13.1-go1.27.2` rebuilds the checksum-pinned
upstream 1.13.1 source with Go 1.27.2. The latter replaces the vulnerable Go 1.26.6
standard library without changing CLI flags, UID 65534, socket paths or request policy.
Their contexts are `deploy/images/valkey` and `deploy/images/socket-proxy`; both are
included in the base production build. ChatBotGo must load the same owned Valkey
image for its host architecture. No official-image fallback or live-data mutation
is performed by building these candidates.

## Old-profile-compatible worker baseline (10.2.0)

This release retains the legacy API/alarm settings and `fixed` max-age modes,
including `bot_reply_outbox.dispatch_budget_ms`,
`source_observation.transaction_timeout_ms`,
`youtube_delivery.claim_freshness_window_ms` and
`youtube_delivery.delivery_send_timeout_ms`. Collector settings and its
`per_job`/`bounded`/`fixed` shape are unchanged. Do not copy the later combined
settings schemas, change the secret master, or activate role/HBA/WAL/Valkey/pool
policy as part of this baseline. The full upstream migration manifest through
324, stream-time work, Node and issuer provenance remain part of the artifact.

### Build and CLI prerequisites

- Verify the genuine immutable shared-go `v2.14.0` release before updating the
  five consumer module pins (`hololive-api`, `hololive-alarm-worker`,
  `hololive-shared`, `hololive-youtube-collector`, `hololive-dbtest`) and sums.
  No unpublished external replace or invented tag is a production dependency.
  `v2.13.0` cannot supply source/effective separation. Source preparation alone
  is not a build or runtime acceptance result.
- Retain service-owned strict settings decoders and legacy fixtures. Missing,
  unknown and non-pointer-null settings remain errors. The existing database
  wrapper supplies explicit positive MaxConns without enabling the new acquire
  policy; Hololive does not call `pgstore.Retire`. Disabled source observation
  still constructs its durable-queue sampler, but does no claim/projection
  preparation or maintenance work.
- On kapu, run the owning release/pre-push gates and independent `GOWORK=off`
  production builds after the real pins are installed. Use root/API/alarm
  version `10.2.0`, a clean full 40-character source revision, and the existing
  architecture/Node/issuer manifest checks. Runtime hosts never build.
- Exercise each actual API, alarm and collector binary with
  `STACK_WORKER_PROFILE_FILE=<retained-old-profile> <binary> --check-worker-profile`.
  Unset `STACK_WORKER_PROFILE_SOURCE_FILE` for an isolated decoder check; for
  pairing checks set it to the separate desired path. The effective file is
  always the only settings input. Reject invalid enabled **and disabled**
  profiles with the same candidate CLI before any migration or writer stop.

### Adoption, activation and rollback

1. While each old process still runs and its exact loaded bytes are available,
   prepare its pair with the reviewed helper:
   `python3 scripts/deploy/lib/worker-profile-pair.py prepare SERVICE ABSOLUTE_PAIR_DIR CONTAINER ROOT`.
   Supported Compose services are `hololive-api`, `hololive-alarm-worker`,
   `youtube-collector` (c), and `youtube-collector-b`. First adoption authenticates
   the metrics loaded hash, compares exact bytes, rechecks process identity and
   runs the old image CLI with no network. Missing/mutated bytes or an unpaired
   stopped process fail closed; do not reconstruct a profile from defaults.
2. For native a/d, use the approved
   `scripts/deploy/ap-host-native-deploy.sh HOST --prepare-profile` before any
   mirror update or maintenance stop. It freezes the old binary/profile checksums
   and changes only the host-owned effective/source paths, without restarting.
3. Transfer verified candidate artifacts. Render the existing prod+live-compat
   (central) or prod+Seoul (b) configuration with
   `WORKER_PROFILE_OVERRIDE_FILE=none`, pipe `config --format json` to
   `worker-profile-pair.py candidate SERVICE ABSOLUTE_PAIR_DIR`, then select that
   directory's `compose.yml` using `WORKER_PROFILE_OVERRIDE_FILE`. Run
   `compose.sh ... run --rm --no-deps --pull never SERVICE --check-worker-profile`
   and `worker-profile-pair.py verify-source SERVICE ABSOLUTE_PAIR_DIR`.
   Keep all profiles unchanged for this stage. A CLI check proves no DB/schema
   compatibility; do not run the older migration manifest or implicitly migrate.
4. Activate using `worker-profile-pair.py activate SERVICE ABSOLUTE_PAIR_DIR ROOT`
   and recreate only approved services with `up -d --no-build --no-deps`.
   Central API/alarm use the release runbook's no-build path, **not** the local
   build-and-deploy `compose-redeploy-service.sh`. Existing collector wrappers
   retain issuer-first ordering and provenance checks. Native apply also writes
   host infrastructure, so it is not a read-only CLI preflight.
5. Verify running image/binary revision and authenticated loaded profile hash.
   `verify-live SERVICE PAIR_DIR CONTAINER` checks Compose; `verify-native SERVICE
   RELEASE_DIR UNIT` also checks `/proc/PID/exe`, PID/start identity, checksums
   and effective/source paths. Native deploy/completion invoke this check.
   Prove atomic desired-file replacement is detected within the 60-second
   check interval while effective bytes/hash remain unchanged, for collector
   as well as API/alarm. Use the authorized acceptance procedure; source drift
   reports bounded `profile_file_changed`, never hot reload. Do not migrate to
   incompatible settings until this baseline has actually passed acceptance.

The desired directory is mounted read-only so atomic replacement remains visible;
individual file mounts would pin the old inode. Effective profiles live in the
immutable release mount and `STACK_WORKER_PROFILE_SOURCE_FILE` names only the
desired service slot. Profiles must contain no passwords, env exports or tokens.
Active overlays/receipts under `artifacts/worker-profile` survive deploy-tree sync
and boot. Preserve the release directories, retained image tags and deployment
inputs until acceptance; per-file atomic publication is not a whole-deploy lock.

A prepared pair alone is **not** an API/alarm rollback bundle. Preserve `service`,
`previous-state`, `previous/{profile.json,pair.json,SHA256SUMS,compose.yml}`,
numbered `compose/*.yml` snapshots and `project-directory` as assembled by
`compose-redeploy-service.sh`. Check with `compose-rollback-service.sh BACKUP
--check`; only an approved `--apply` restores the pair. Collector c/b and native
a/d retain their existing rollback entrypoints and stopped-state behavior.
Rollback never writes the desired mirror or synthesizes removed keys; verify
the restored loaded hash, not health alone. A known schema-incompatible old
writer must stay stopped even when its profile CLI succeeds.

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
HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE=/etc/stack-secrets/hololive-bot/youtube-collector.env
```

### Central endpoint ownership

AP overlays never hardcode the central address. All three (`docker-compose.seoul.yml`,
`docker-compose.osaka.yml`, `docker-compose.osaka2.yml`) read one key family and fail the
render when it is absent, so a host move cannot leave an AP silently pointed at the
retired address:

```text
HOLOLIVE_CENTRAL_POSTGRES_HOST   required
HOLOLIVE_CENTRAL_POSTGRES_PORT   default 5433
```

AP collector overlays do not interpolate `HOLOLIVE_CENTRAL_CACHE_HOST`. Collector rendered services have no `CACHE_*` env, no `valkey-cache` depends_on, and no `valkey-cache-socket` mount. Central `docker-compose.prod.yml` still owns `x-cache-env` for `hololive-api` / `hololive-alarm-worker`. `CACHE_PASSWORD` remains required to interpolate the shared `valkey-cache` service definition from the base file.

`ap-compose.env` owns those values. `CLIPROXY_BASE_URL` comes from the same file through
`docker-compose.prod.yml`; AP overlays must not re-declare it. Host-native APs
(`youtube-collector-a`/`-d`) get PostgreSQL from `AP_POSTGRES_HOST` in
`scripts/deploy/ap-host-native-deploy.sh` and do not emit `CACHE_*` lines.

AP overlays use only `youtube-collector.env` for `youtube-collector` instances, so Iris
egress tokens stay out of AP collector containers. Osaka/Seoul AP hosts also use
`ap-compose.env`, which excludes `IRIS_WEBHOOK_TOKEN` and `IRIS_BOT_TOKEN`.
The central unsuffixed `youtube-collector` (`c`) also uses scoped
`youtube-collector.env`; it still must not receive Iris egress tokens or the
monolithic Compose env file as an `env_file`.

`youtube-collector.env` must provide a non-empty `METRICS_API_KEY`, the shared
`HOLOLIVE_OTLP_GRPC_ENDPOINT`, and `OTEL_YOUTUBE_COLLECTOR_<slot>_ENABLED=true`
for every deployed slot. Host-native `a`/`d` bind metrics to their tailnet
address, so omitting `METRICS_API_KEY` is a startup configuration error rather
than a keyless metrics mode.

`HOLOLIVE_OTLP_GRPC_ENDPOINT` uses the gRPC `host:port` form. The generic
`OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` variables
are rejected instead of being reinterpreted with a second URL grammar. This is a
permanent contract, not a retirement guard: it has no removal condition
(`DEC-20260926-hololive-legacy-env-config-retirement`).

The live API does not join `observability-traces`. Its approved host/Tailscale OTLP
endpoint must be explicitly present in `compose.env`; a container DNS endpoint on
the removed network will not work. Other observability peers cannot reach the API
through that external network. Admin/LLM loopback bindings remain unchanged.
`HOLOLIVE_ADMIN_API_PORT_BIND_IP` must match `tailscale ip --assert` on the deployment
host before any build/start/restart. The approved current IPv4 binding remains
`100.100.1.8:30006/udp`. Admin sources are loopback and Iris Admin `100.100.1.5/32`;
broad private CIDRs and wildcard source overrides fail preflight.

AP collectors no longer use Valkey (see the collector cache-topology contract).
The obsolete tailnet 6379 publish is removed; central private Unix socket, internal
network/password authentication and loopback remain. PostgreSQL AP access retains
TLS with `verify-full`/SCRAM and the existing exact host source list from `pg_hba.conf`.
The tracked ingress nftables policy now filters both host input and Docker DNAT
forward paths for admin UDP 30006 and PostgreSQL TCP 5433. Mutation preflight requires
the reviewed installed file, active firewall unit, and loaded DB source/forward rules.
An empty source set, stale unit, or unavailable firewall inspection stops cutover.
Applying the firewall, host OTLP configuration, or Compose changes requires the
separately authorized service deployment workflow; source validation activates none of them.

Collector compatibility env pairs use `${NEW-${OLD-default}}` / `${OLD-${NEW-default}}` interpolation. This preserves explicit empty values so the collector loader fails closed, while unset, new-only, old-only, equal, and conflicting inputs retain the truth table documented in [`youtube-collector.md`](../../docs/current/runbooks/youtube-collector.md#key-environment-variables).

Deploy this repo-side contract after `tools/sync-host.sh <host> --apply` has mirrored
`compose.env` or `ap-compose.env` plus the per-service env files to the target host.

`scripts/deploy/lib/postgres-capacity.sh`가 production mutation entrypoint의 공통 owner입니다.
`scripts/deploy/compose.sh ... up`, `build-all.sh`, `scripts/deploy/compose-redeploy-service.sh`는
build/migration/up보다 먼저 이 gate를 호출해 `COMPOSE_ENV_FILE`의 PostgreSQL pool override key만 읽고
stack 전체 connection budget을 다시 계산합니다. Target-rendered allocation이 `max_connections=60`에서 superuser 예약
3(`@superuser-reserved`)을 뺀 비슈퍼유저 슬롯 대비 최소 2개 reserve를 남기지 않으면 어떤 표준 배포 경로도 진행하지 않습니다. 이 배포-host preflight는 Bash와 전송된 policy만 사용하며 CI용 uv/Python runtime을 요구하지 않습니다. Default policy만 확인할 때는
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
`youtube-collector-c`, `hololive-db-migrate`, Osaka `youtube-collector-a`,
Osaka2 `youtube-collector-d`, and Seoul `youtube-collector-b`. The retired
insecure downgrade ledger stays closed by preserving production paths with
verified TLS and the CA bundle above.

## Requirements

- Docker Compose v2.24.4+ — 오버레이의 `!override` YAML 태그가 이 버전부터 지원된다.
  build `provenance`/`sbom` 속성도 지원해야 한다.
- Scheduled/manual final-image scanning uses `docker-compose.security-scan.yml` only to disable
  attestations that the hosted runner's disposable Docker image store cannot retain. The scanned
  runtime layers and image tags remain the production build targets; ordinary production builds
  keep `provenance: mode=max` and `sbom: true`.
- BuildKit 활성 Docker Engine — Dockerfile들의 `# syntax=docker/dockerfile:1.28.0@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b`
  (cache mount, `COPY --link`, per-Dockerfile `.dockerignore`) 전제.
- production Go build는 `GOWORK=off`로 각 `go.mod`의 stable published external pin만 사용한다.
  로컬 sibling checkout은 image source가 아니다.
- 호스트 호환성 확인: `docker compose -f deploy/compose/docker-compose.prod.yml config` 가
  에러 없이 렌더되는지로 검증한다.
