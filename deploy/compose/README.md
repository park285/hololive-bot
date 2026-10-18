# Compose Files

- `docker-compose.prod.yml`: production baseline for the main host.
- `docker-compose.live-compat.yml`: live wiring overlay the main host always applies with the production
  baseline (ports, volumes, and the main-host `youtube-collector-c`, which is `youtube-collector` in
  `docker-compose.prod.yml`).
- `docker-compose.collector.yml`: shared collector/issuer definitions only; no central credentials.
- `docker-compose.osaka.yml`: standalone AP composition for collector/issuer `a` (native runtime remains actual).
- `docker-compose.osaka2.yml`: standalone AP composition for collector/issuer `d` (native runtime remains actual).
- `docker-compose.seoul.yml`: standalone AP composition for collector/issuer `b`.
- `docker-compose.remote-cache.yml`: optional BuildKit registry cache overlay. Export is fixed to
  `mode=min`, so only final-image cache records are published; intermediate build stages and copied
  source trees must not be exported by this operational overlay.

Prefer repository wrappers over raw `docker compose`:

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml config
./scripts/deploy/compose-redeploy-service.sh <service>
```

AP uses only its host file, for example `COMPOSE_PROFILES=oracle COMPOSE_ENV_FILE=<ap-env> ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.seoul.yml config --quiet`.
Do not combine AP with the production file or select the collector base itself: `extends`
reuses definitions without starting central `c`. Central production adds only its DB/migration dependencies.
The transfer and rollback sets retain the collector base beside the selected host file.
The recorded previous layout, image and checked profile are restored together; an old
production-based rollback may still need old interpolation keys. Removing AP central keys
from the secret master/mirror requires separate approval after that rollback window closes.

## Worker profile release pairing

Worker profiles are non-secret artifacts. Compose mounts only
`/etc/stack-secrets/hololive-bot/worker-profiles` read-only as a directory; certificate
and credential mounts remain individually scoped. Atomic mirror replacement is therefore
visible to the worker-profile drift checker. The live overlay changes only `/app/logs`
through normal Compose volume-target merging.

Central API/alarm deployment and the paired central/Seoul collector cutovers run the
candidate's `--check-worker-profile` with the actual rendered service mounts before
migration or writer replacement. They retain the checked exact bytes, image identity,
revision/architecture and checksums under the deployment's release/backup directory.
The running container labels identify this retained pair.
`artifacts/worker-profile/active/*.yml` keeps the selected pair across wrapper-driven
reboot/recreate. These root-owned regular files are verified as part of the boot execution
tree and preserved by exec-tree sync; they are not writable runtime backups or source-transfer
inputs. Do not delete these artifacts or retained image
tags during the rollback window.

Rollback checks the previous image against its retained profile, not the current mirror.
The retained profile is mounted at `/run/hololive-bot/worker-profile-release/profile.json`,
while the canonical profile directory stays mounted at `/run/hololive-bot/worker-profiles`.
`STACK_WORKER_PROFILE_FILE` selects the effective release artifact;
`STACK_WORKER_PROFILE_SOURCE_FILE` selects the canonical, observation-only drift source.
Rollback never copies profile bytes into `/etc/stack-secrets`. A desired/effective mismatch
is diagnostic and does not prevent a valid previous pair from starting.

Central API/alarm manual rollback is
`scripts/deploy/compose-rollback-service.sh <printed-backup-directory> --check`, then
`--apply` within the approved rollback scope. Collector rollback continues through
`po-central-cutover.sh rollback <backup>` or `ap-rollback.sh seoul --apply` with its
existing approval variables. An originally stopped collector is recreated without
starting it; schema compatibility must be restored before an explicit start.

Central capture requires a valid `collector-prechange.state` in its completed backup;
missing state fails closed rather than assuming the old writer was running. A restoration
activation failure is reported before removing the issuer. Retain that backup and inspect
the installed candidate files before explicit recovery; a failed restoration is not a
successful source rollback.

Preparation can load candidate image tags before writer cutover is armed. If image/profile
validation then fails, central and AP restore both source and the recorded tag baseline:
present tags return to their rollback references; tags absent at baseline are removed by
exact reference. This includes AP first deployment and an issuer-absent baseline.
Failed inventory lookup is not treated as absence. A failed lookup/removal is reported
while preserving the original deployment exit status; no automatic retry, runtime restart,
container/volume deletion, receipt invention or rollback-archive deletion occurs in this path.

AP rollback check renders and validates a temporary overlay without rewriting the retained
pair. Apply publishes those exact validated bytes, still referencing the retained release
directory. Publication uses invocation-owned temporary files in each destination directory,
staging all outputs before individual renames and publishing the pair pointer last.
Abandoned fixed `.next` files are not deleted or reused. These renames do not form an atomic
multi-file transaction or serialize overlapping deployments.

### First adoption, before profile sync or maintenance stop

An unpaired old runtime cannot be reconstructed from a mutated mirror. The first capture
requires a running old process: the credential-consuming healthcheck reads the loaded
`profile_hash` from `/metrics`, the captured bytes must match it, and the old binary must
accept those bytes. A missing identity, changed process, mismatch, or stopped unpaired
runtime fails without changing the writer. No credential value is printed or saved.
The explicit service selects `API_SECRET_KEY` for API/alarm metrics and
`METRICS_API_KEY` for collector metrics. There is no credential fallback.

For a separately approved preparation on a Compose host, run from its deployment root:

```bash
sudo python3 scripts/deploy/lib/worker-profile-pair.py prepare \
  youtube-collector-b "$PWD/artifacts/worker-profile/releases/prepared-b-$(date -u +%Y%m%dT%H%M%SZ)" \
  hololive-youtube-collector-b "$PWD"
```

Use `youtube-collector` / `hololive-youtube-collector-c` centrally, or `hololive-api` /
`hololive-api` and `hololive-alarm-worker` / `hololive-alarm-worker`. Preparation preserves
the original container identity in the receipt, retains its image, and does not restart it.
The ordinary deployment can also perform this first capture while the original bytes are
still available. Before a maintenance-stopped deployment, preparation must already exist.

Native a/d use `ap-host-native-deploy.sh <host> --prepare-profile` from kapu with the
host's existing deploy approval variable. This does not build or stop a service. It proves
the running executable and loaded hash, validates the old binary, then freezes
`worker-profile.json` and `SHA256SUMS` inside that release and updates only the host-owned
`youtube-collector-host.env` to its release path plus canonical source path. Normal native
deployment freezes each candidate the same way; manual and automatic rollback validate
the previous release's checksums and binary/profile pair before starting it.

**11.0.0 prerequisite:** deploy and actually accept the old-profile-compatible
10.2.0 binary with `STACK_WORKER_PROFILE_SOURCE_FILE` before incompatible settings
changes. Preparation receipts alone do not prove that baseline was accepted.

Local verification ownership: run the Compose H3/mount render checks, existing native/AP
cutover and maintenance-stopped behavior tests, and real candidate/previous-binary smoke
checks for invalid settings, missing/corrupt pairs, profile-only changes, atomic mirror
replacement, and failed cutover restoration. No live operation is implied by these edits.


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

## Worker settings cutover (11.0.0)

The preceding **10.2.0 acceptance is mandatory**: its API/alarm binaries retain
the old settings and modes while adding immutable effective/source separation.
Accept its actual CLI, authenticated loaded hash, atomic desired-file drift and
paired rollback before changing the profile master or activating new DB policy.
This source tree does not claim that live acceptance has happened.

11.0.0 uses only the final strict API/alarm settings schema and modes. Removed
settings (`bot_reply_outbox.dispatch_budget_ms`,
`source_observation.transaction_timeout_ms`,
`youtube_delivery.claim_freshness_window_ms`, and
`youtube_delivery.delivery_send_timeout_ms`) must not be supplied to the new
binary; there is no legacy decoder shim. Collector profile settings retain their
`per_job`/`bounded`/`fixed` shape. Keep old exact bytes only in the previous
release pair, not as extra keys or aliases in the new profile.

### Build and CLI prerequisites

- All five consumers retain the genuine published shared-go `v2.14.0` pins and
  sums. No unpublished external replace or invented tag is a production input.
- On kapu, run the owning release/pre-push gates and independent `GOWORK=off`
  production builds. Use root/API/alarm version `11.0.0`, a clean full
  40-character source revision, and the architecture/Node/issuer manifests.
  Runtime hosts never build.
- Preserve the full upstream migration manifest through **324**, including
  stream-duration, time-finalization and membership behavior. This worker/DB
  operations cutover adds no data migration. Never run an old manifest ending
  at 321 or roll back already-applied migrations to make an old image start.
- Run each actual candidate binary's `--check-worker-profile` on the final
  candidate bytes, including invalid enabled and disabled profile cases. The
  effective file is the only settings input; the separate desired path is
  observation-only. Disabled source observation retains its queue sampler but
  performs no claim/projection preparation writes.

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
3. After 10.2.0 acceptance and retention of each previous pair, sync the approved
   final desired profiles and transfer verified 11.0.0 candidate artifacts.
   Render prod+live-compat (central) or standalone `docker-compose.seoul.yml` (b) with
   `WORKER_PROFILE_OVERRIDE_FILE=none`, pipe `config --format json` to
   `worker-profile-pair.py candidate SERVICE ABSOLUTE_PAIR_DIR`, then select that
   directory's `compose.yml` using `WORKER_PROFILE_OVERRIDE_FILE`. Run
   `compose.sh ... run --rm --no-deps --pull never SERVICE --check-worker-profile`
   and `worker-profile-pair.py verify-source SERVICE ABSOLUTE_PAIR_DIR`.
   A CLI check proves no DB/schema compatibility; never implicitly migrate.
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
   reports bounded `profile_file_changed`, never hot reload. Keep baseline and
   final acceptance records separate; source integration is not live acceptance.

The desired directory is mounted read-only so atomic replacement remains visible;
individual file mounts would pin the old inode. Effective profiles live in the
immutable release mount and `STACK_WORKER_PROFILE_SOURCE_FILE` names only the
desired service slot. Profiles must contain no passwords, env exports or tokens.
Active overlays/receipts under `artifacts/worker-profile` survive deploy-tree sync
and boot. Preserve the release directories, retained image tags and deployment
inputs until acceptance; per-file atomic publication is not a whole-deploy lock.

A prepared pair alone is **not** an API/alarm rollback bundle. Preserve `service`,
`previous-state`, `previous/{profile.json,pair.json,SHA256SUMS,compose.yml}`,
numbered `compose/*.yml` snapshots, the sibling collector base used by `extends`,
and `project-directory` as assembled by
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

AP rendering requires the dedicated scraper password, explicit primary endpoint and the
collector inputs only; `DB_PASSWORD`, `HOLOLIVE_DB_PASSWORD`, `CACHE_PASSWORD`, central
runtime env files and alarm version are not interpolated. The collector has no cache mounts
or DB/cache dependencies. `ap-compose.env` owns the endpoint; the non-secret primary address
owner is `deploy/topology/hosts.conf` (`central=100.100.1.8`). Native generation reads that
owner and `scripts/ci/postgres-capacity-policy.tsv`, not a second address/pool literal.
CLIProxy/OTLP workstation addresses are unrelated to the primary DB route.

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
TLS with `verify-full`/SCRAM. Role-specific HBA rules remain restrictive even when Docker
source NAT presents AP traffic as a bridge gateway; source-IP rules alone are not isolation.
The tracked ingress nftables policy now filters both host input and Docker DNAT
forward paths for admin UDP 30006 and PostgreSQL TCP 5433. Mutation preflight requires
the reviewed installed file, active firewall unit, and loaded DB source/forward rules.
An empty source set, stale unit, or unavailable firewall inspection stops cutover.
Applying the firewall, host OTLP configuration, or Compose changes requires the
separately authorized service deployment workflow; source validation activates none of them.

## Prepared database policy (not activated)

PostgreSQL remains `max_connections=60`, with three superuser slots. The old application/
migrator allocation 55 plus exporter 1 and nightly 2 was 58 against 57 ordinary slots.
The local uniform collector maximum 7 (minimum 2 unchanged) gives runtime 22 + scraper 28
+ migrator 1 + exporter 1 + nightly 2 = 54, leaving **three ordinary control slots**.
Both capacity readers account for external consumers and aggregate all pools by role;
native max is emitted from the same policy. Role limits are runtime 25 (including bounded
maintenance/replay control work), scraper 28, migrator 1, exporter 1 and nightly 2.
One migration job at a time is supported. Multiple runtime maintenance commands must share
the three-slot control allowance, not claim independent three-slot budgets.

**Collector activation prerequisite:** all four observed worker counts are below 7 and
provider limits do not exceed workers, and minimum 0 remains valid. These do not prove
contention safety: renewals, readiness/discovery and publish/finalize overlap. Representative
pool wait/renewal/finalization stress under WAN latency and canceled operations must pass
before activating 7. Sampled production DB maximum 46 is not a capacity guarantee.
No increase of `max_connections` is prepared. Only the rollout owner applies
`postgres/role-limits.sql` through the approved admin socket after **all four**
collectors are accepted with max 7; capture old limits for rollback. Deployment
wrappers do not activate role limits. Fresh initialization sets app role limits;
existing PGDATA does not rerun init-db.
Existing env overrides of collector maximum 8 must be migrated under separate approval;
the policy rejects them rather than pretending the new default overrides deployed inputs.
An old-layout/image rollback must retain a reviewed capacity-safe pool input (including
native host.env), or stop for a new capacity decision; restoring the old 58-slot allocation
is not made safe by restoring a valid binary/profile pair.

The central migration job and its admin bootstrap use `/var/run/postgresql` with explicit
`PGSSLMODE=disable` and `network_mode: none`. Only DB/migrator mount the private tmpfs volume;
ordinary runtime containers never do. Operator/health/nightly `docker exec` sockets remain
available. The Go transport validation and socket mount must ship **before/with** HBA denying
admin/migrator SQL and replication TCP. Network clients retain verify-full; do not loosen
network TLS to repair a socket rollout. The central collector migration-ledger proof also
uses this socket after proving the collector targets the local primary. Slot/process
`PGAPPNAME` values are set by deployment; providers append pool identity for multi-pool API.
Scraper identity is fixed to `hololive_scraper`, matching migration grants, the runtime
role constant and HBA. The configurable scraper-name input is removed by the approved
local contract cutover. A differently named existing login requires separately approved
credential/ownership migration; there is no membership alias or automatic role rename.
There is no active replication TCP allow rule, including the retired `.5` standby route.
Standby reactivation requires a separately approved explicit TLS transport/address rule;
the retained standby Compose file is not authorization to enable replication access.

Server command flags prepare TCP keepalive 60/10/6, lock waits, all temp files, slow statements
at 1s, and bind-parameter lengths 0 for normal/error logging. SQL literals/comments can still
be sensitive; verify synthetic password/payload non-disclosure, retention and access controls.
No global runtime lock timeout is introduced. Existing WAL settings are explicitly encoded:
checkpoint 900s, max WAL 2GB, compression lz4, also in the inactive standby reference.
Persisted `postgresql.auto.conf` cleanup is a **separately approved operation**: capture old
values/sources and rollback flags, remove only the three approved entries (never RESET ALL),
then verify `source='command line'` and `pending_restart` after approved recreation.

**Valkey activation BLOCKED:** eviction policy and maxmemory are unchanged. There is no real
Valkey scrape/alert rule and the supplied memory queries were empty. The existing application
client is not an exporter. Introducing an exporter would require an unapproved new runtime
dependency; a client-owned INFO collector and matching platform scrape/alerts are not present.
Before noeviction or a memory increase: install a real authenticated scrape and alerts for
scrape failure, used/max memory, evictions, rejected writes/OOM and headroom; collect workload
coverage, budget non-evictable overhead, and explicitly accept write failures at the cap.
Different logical DB numbers do not isolate instance-wide eviction. Nothing here claims readiness.


Collector compatibility env pairs use `${NEW-${OLD-default}}` / `${OLD-${NEW-default}}` interpolation. This preserves explicit empty values so the collector loader fails closed, while unset, new-only, old-only, equal, and conflicting inputs retain the truth table documented in [`youtube-collector.md`](../../docs/current/runbooks/youtube-collector.md#key-environment-variables).

Deploy this repo-side contract after `tools/sync-host.sh <host> --apply` has mirrored
`compose.env` or `ap-compose.env` plus the per-service env files to the target host.

`scripts/deploy/lib/postgres-capacity.sh`가 production mutation entrypoint의 공통 owner입니다.
`scripts/deploy/compose.sh ... up`, `build-all.sh`, `scripts/deploy/compose-redeploy-service.sh`는
build/migration/up보다 먼저 이 gate를 호출해 `COMPOSE_ENV_FILE`의 PostgreSQL pool override key만 읽고
stack 전체 connection budget을 다시 계산합니다. Target-rendered allocation이 `max_connections=60`에서 superuser 예약
3(`@superuser-reserved`)을 뺀 비슈퍼유저 슬롯 대비 최소 3개 control reserve를 남기지 않으면 어떤 표준 배포 경로도 진행하지 않습니다. 이 배포-host preflight는 Bash와 전송된 policy만 사용하며 CI용 uv/Python runtime을 요구하지 않습니다. Default policy만 확인할 때는
`scripts/ci/check-postgres-capacity.sh`를, 특정 render 결과를 확인할 때는 세 번째 인자로 해당
Compose env file을 전달합니다. 이 검사는 다른 env 값이나 secret을 출력하지 않습니다.

## PostgreSQL TLS

`holo-postgres` serves TLS with `ssl=on`. The `stack-secrets` master owns the server
certificate and key; the host copy lives at `/etc/stack-secrets/hololive-bot/postgres-tls/`
and mounts read-only at `/run/hololive-bot/postgres-tls/`, outside the client-mounted
`certs/` directory. Reissuance sends `SIGHUP` to `holo-postgres` so PostgreSQL reloads
the server material without a container recreate.

Network PostgreSQL clients use `verify-full`: central API, alarm-worker and collector `c`,
and remote collectors `a/b/d`, with the approved CA bundle. Central API has multiple
plane-specific pools, not five separate central runtime processes. The isolated central
`hololive-db-migrate`/admin bootstrap deliberately uses a Unix socket with `sslmode=disable`;
that is not an insecure TCP downgrade. Exporter retains its dedicated TLS route.
Fresh-PGDATA psql initialization is a separate controlled bootstrap boundary: its session
suppresses normal/slow/error statement logging while setting role passwords; the ordinary
Go bootstrap sends client-generated SCRAM verifiers. Synthetic failure/log checks are required.

Local acceptance (not run by this preparation): `bash scripts/deploy/test-compose-postgres-routing.sh`,
`bash scripts/ci/check-postgres-capacity.sh`, and
`bash scripts/deploy/test-postgres-access-policy.sh`. The latter requires the already-built
repository PG18.6 image (`POSTGRES_POLICY_TEST_IMAGE` may select its tag), Docker and OpenSSL;
it uses a disposable internal network, tmpfs database/socket and synthetic certificates only,
publishes no ports, and proves HBA rejection, runtime/exporter TLS, migration/nightly socket
access and aggregate role limits. It never reads runtime env files or pulls/builds images.

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
