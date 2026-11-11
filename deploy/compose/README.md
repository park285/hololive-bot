# Shared runtime contracts

이 디렉터리에는 실행 정의가 더 이상 없습니다. 중앙(hololive-seoul)은 [deploy/quadlet](../quadlet/README.md),
Seoul AP collector b는 2026-11-06부터 iris-seoul rootful Podman Quadlet([deploy/podman/iris-seoul](../podman/iris-seoul/README.md)),
Osaka a/d는 host-native systemd입니다. Docker Compose AP 정의와 `compose.sh`·`ap-deploy.sh`는 그 전환으로 지웠습니다.

> 아래 절은 AP와 중앙에 함께 걸린 계약(worker profile, DB·TLS 정책, env 소유)을 다룹니다. unit 정의·설치·
> 재시작 명령은 위 README들과 `docs/current/runbooks/`가 소유합니다.

## Worker profile release pairing

Worker profiles are non-secret artifacts. The Quadlet units (central and iris-seoul) mount only
`/etc/stack-secrets/hololive-bot/worker-profiles` read-only as a directory; certificate
and credential mounts remain individually scoped. Atomic mirror replacement is therefore
visible to the worker-profile drift checker.

Every candidate runs its `--check-worker-profile` before migration or writer replacement.
Quadlet cutovers use `worker-profile-pair.py check SERVICE PAIR_DIR`, which runs the pair image
with only the release directory and `STACK_WORKER_PROFILE_FILE` (the only input that CLI reads)
and no network. They retain the checked exact bytes, image identity,
revision/architecture and checksums under the deployment's release/backup directory.
The running container labels identify this retained pair.
The `/etc/containers/systemd/hololive-bot/<unit>.container.d/50-worker-profile.conf`
drop-in keeps the selected pair across reboot/recreate (`Image=sha256:<id>`). These root-owned regular files are preserved by deploy-tree
sync and `sync-opt-current.sh`; they are not writable runtime backups or source-transfer
inputs. Do not delete these artifacts or retained image
tags during the rollback window.

Rollback checks the previous image against its retained profile, not the current mirror.
The retained profile is mounted at `/run/hololive-bot/worker-profile-release/profile.json`,
while the canonical profile directory stays mounted at `/run/hololive-bot/worker-profiles`.
`STACK_WORKER_PROFILE_FILE` selects the effective release artifact;
`STACK_WORKER_PROFILE_SOURCE_FILE` selects the canonical, observation-only drift source.
Rollback never copies profile bytes into `/etc/stack-secrets`. A desired/effective mismatch
is diagnostic and does not prevent a valid previous pair from starting.

Central API/alarm manual rollback regenerates the previous pair's drop-in with
`worker-profile-pair.py rollback SERVICE PREVIOUS_PAIR_DIR` (hololive-api also pins the previous
`APP_VERSION`), publishes it with `activate`, then runs `systemctl daemon-reload` and restarts only
the approved unit. Collector rollback continues through
`po-central-cutover.sh <c|b> rollback <backup>` with its slot's approval variable. The Quadlet
cutover refuses a stopped collector because Quadlet removes the container of a stopped unit,
so the previous profile can only be proven from a running process.

A central restoration activation failure is reported before any unit restart. Retain that
backup and inspect the installed drop-in before explicit recovery; a failed restoration is not
a successful rollback.

The Quadlet cutover loads archives under `:candidate` tags and moves `:prod`, the drop-in and
the issuer receipt only at cutover, so a failed candidate check leaves them unchanged.
Failed inventory lookup is not treated as absence. A failed lookup/removal is reported
while preserving the original deployment exit status; no automatic retry, runtime restart,
container/volume deletion, receipt invention or rollback-archive deletion occurs in this path.

Pair publication uses invocation-owned temporary files in each destination directory,
staging all outputs before individual renames and publishing the pair pointer last.
Abandoned fixed `.next` files are not deleted or reused. These renames do not form an atomic
multi-file transaction or serialize overlapping deployments.

### First adoption, before profile sync or maintenance stop

An unpaired old runtime cannot be reconstructed from a mutated mirror. The first capture
requires a running old process: the credential-consuming healthcheck reads the loaded
`profile.hash` from bounded `/diagnostics/workers` v1 JSON, the captured bytes must match it, and the old binary must
accept those bytes. A missing identity, changed process, mismatch, or stopped unpaired
runtime fails without changing the writer. No credential value is printed or saved.
The explicit service selects `API_SECRET_KEY` for API/alarm diagnostics and
`METRICS_API_KEY` for collector diagnostics. There is no credential fallback.

For a separately approved preparation, run from the deployment root (central
`/opt/hololive-bot/compose/current`, iris-seoul `/opt/hololive-bot/collector-b/current`, where the tool is copied from the
reviewed revision because that root holds no source tree):

```bash
sudo python3 scripts/deploy/lib/worker-profile-pair.py prepare \
  youtube-collector "$PWD/artifacts/worker-profile/releases/prepared-c-$(date -u +%Y%m%dT%H%M%SZ)" \
  hololive-youtube-collector-c "$PWD"
```

Use `youtube-collector-b` / `hololive-youtube-collector-b` on iris-seoul, or `hololive-api` /
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
10.2.1 binary with `STACK_WORKER_PROFILE_SOURCE_FILE` before incompatible settings
changes. Preparation receipts alone do not prove that baseline was accepted.

Local verification ownership: run the existing native and Quadlet cutover behavior tests, and real candidate/previous-binary smoke
checks for invalid settings, missing/corrupt pairs, profile-only changes, atomic mirror
replacement, and failed cutover restoration. No live operation is implied by these edits.


## Owned ingress image

`admin-dashboard-ingress` uses the local-only `localhost/iris-stack-nginx:1.31.8-zlib1.4-r0`
image (`Pull=never`). Its only build context is `deploy/images/nginx`; the
immutable official Nginx base retains its entrypoint, configuration and users,
with Alpine 3.24 `zlib=1.4-r0` (CVE-2026-4738) and `musl=1.2.6-r3`/`musl-utils=1.2.6-r3`
(CVE-2026-107891) applied. `ca-certificates-bundle=20261020-r0` keeps the CA bundle free of the
roots distrusted on 2026-10-22 (emSign ECC Root CA - G3, AAA Certificate Services), so the
built image needs no separate CA derivation. The PostgreSQL and Valkey runtimes apply the same
zlib, musl and CA bundle pins; PostgreSQL adds `brotli-libs=1.2.1-r0`, `libcurl=8.23.0-r0` and
`nghttp2-libs=1.70.0-r0`.

`./build-all.sh --build-only` builds it with `podman build` together with the other central
images. For isolated local builds without runtime env inputs:

```bash
podman build --platform linux/arm64 --format docker --build-arg REVISION=unknown \
  --tag localhost/iris-stack-nginx:1.31.8-zlib1.4-r0 deploy/images/nginx
```

Use the full committed image-source revision instead of `unknown` only after the
source is committed. Other stack ingress consumers must load this same owned
image rather than maintain a second recipe or pull the unpatched official tag.
The existing ingress behavior checks build an isolated temporary tag from this
context; `NGINX_TEST_PLATFORM` selects the architecture (default `linux/amd64`).

`valkey-cache` also uses a local-only owned image:
`localhost/iris-stack-valkey:9.1.3-zlib1.4-r0` applies the same zlib, musl and CA bundle pins to the pinned official
Valkey base. Its context is `deploy/images/valkey` and it is included in `build-all.sh --build-only`.
ChatBotGo must load the same owned Valkey image for its host architecture. No official-image
fallback or live-data mutation is performed by building these candidates.

Valkey 9.1.3은 잘못된 `RESTORE` 문자열 길이로 서버가 종료되는 결함과
Cluster Pub/Sub의 힙 버퍼 오버플로를 수정합니다. 버전 업데이트는 기존
인증·소켓·메모리 정책을 유지하며 Cluster나 Sentinel 전환을 포함하지 않습니다.
`appendonly no`·`save ""` 구성의 재생성은 메모리 상태를 지우므로,
소비자별 데이터 보존 계약을 확인한 뒤 승인된 전환 절차를 적용합니다.

## Worker settings cutover (11.0.0)

The preceding **10.2.1 acceptance is mandatory**: its API/alarm binaries retain
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

- 다섯 소비자는 shared-go `v2.19.0`과 공식 checksum을 고정합니다.
  api·alarm-worker·shared는 `github.com/park285/iris-client-go/v5 v5.0.0`을 직접,
  collector는 간접 의존성으로 고정합니다. 두 정식 tag와 공식 Go 도구로 생성한 checksum이
  운영 빌드 전에 있어야 하며, 미게시 외부 replace나 가짜 tag·checksum은 운영 입력이 아닙니다.
  SDK v5는 H3 `GET /auth/epoch`를 지원하는 Iris와 같은 배포 창에서 교체하며,
  롤백도 인증 호환 조합으로 수행합니다. webhook v3와 inbox 저장 형식은 바꾸지 않습니다.
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
   Supported Quadlet services are `hololive-api`, `hololive-alarm-worker`,
   `youtube-collector` (c), and `youtube-collector-b`. First adoption authenticates
   the bounded `/diagnostics/workers` v1 `profile.hash`, compares exact bytes, rechecks process identity and
   runs the old image CLI with no network. Missing/mutated bytes or an unpaired
   stopped process fail closed; do not reconstruct a profile from defaults.
2. For native a/d, use the approved
   `scripts/deploy/ap-host-native-deploy.sh HOST --prepare-profile` before any
   mirror update or maintenance stop. It freezes the old binary/profile checksums
   and changes only the host-owned effective/source paths, without restarting.
3. After 10.2.1 acceptance and retention of each previous pair, sync the approved
   final desired profiles and transfer verified 11.0.0 candidate artifacts.
   Pipe the installed unit to `worker-profile-pair.py candidate SERVICE
   ABSOLUTE_PAIR_DIR sha256:<id> < /etc/containers/systemd/hololive-bot/<unit>.container`
   and run `worker-profile-pair.py check SERVICE ABSOLUTE_PAIR_DIR`, then
   `worker-profile-pair.py verify-source SERVICE ABSOLUTE_PAIR_DIR`.
   A CLI check proves no DB/schema compatibility; never implicitly migrate.
4. Activate using `worker-profile-pair.py activate SERVICE ABSOLUTE_PAIR_DIR ROOT`.
   Run `systemctl daemon-reload` and restart only the approved units through the release
   runbook's Quadlet path. Existing collector wrappers
   retain issuer-first ordering and provenance checks. Native apply also writes
   host infrastructure, so it is not a read-only CLI preflight.
5. Verify running image/binary revision and authenticated loaded profile hash.
   `verify-live SERVICE PAIR_DIR CONTAINER` checks the Podman container; `verify-native SERVICE
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
Active overlays/receipts under `artifacts/worker-profile` and the central `50-worker-profile.conf`
drop-ins survive deploy-tree sync and boot. Preserve the release directories, retained image tags and deployment
inputs until acceptance; per-file atomic publication is not a whole-deploy lock.

A prepared pair alone is **not** an API/alarm rollback bundle. Preserve the previous pair
directory (`previous/{profile.json,pair.json,SHA256SUMS}`), its retained image tag and the unit
revision it ran under. `worker-profile-pair.py verify SERVICE PREVIOUS_PAIR_DIR` and `check`
prove the pair; only an approved `rollback`/`activate` plus unit restart restores it. Collector c/b
and native a/d retain their existing rollback entrypoints.
Rollback never writes the desired mirror or synthesizes removed keys; verify
the restored loaded hash, not health alone. A known schema-incompatible old
writer must stay stopped even when its profile CLI succeeds.

## 중앙 Valkey 영속 TLS·Sentinel 전환

중앙 Quadlet은 Valkey HA 노드(`valkey-cache.container`)와 Sentinel 접속만 정의합니다. `sync-opt-current.sh`는
compose.env에 `HOLOLIVE_VALKEY_HA_ENABLED=1`과 비어 있지 않은 `VALKEY_SENTINEL_ADDRS`가 없으면 unit을 설치하지 않으며,
옛 standalone/UDS나 Sentinel 없는 직접 TLS 단계의 정의는 남아 있지 않습니다.
`VALKEY_BIND_IP=100.100.1.8`과 검증된 `VALKEY_IMAGE`를 지정합니다.
`valkey-cache` 물리 슬롯은 `hololive-bot_valkey-ha-data`를 사용하며 역할은 native `/data/valkey.conf`가 소유합니다.

두 그룹 primary와 각 복제본 2개의 초기 동기화를 수용하고 세 Sentinel/quorum 2를 확인한 뒤
`VALKEY_SENTINEL_ADDRS=100.100.1.3:26379,100.100.1.8:26379,100.100.1.5:26379`를 설정합니다.
앱 unit은 직접 host/socket을 비우고 master `hololive`, discovery 계정 `hololive-discovery`로 접속합니다.
직접 endpoint fallback은 없습니다. worker profile drop-in(`50-worker-profile.conf`)은 image·profile 경로만 바꾸며
`LIVE_LOGS_PATH`의 로그 bind는 기본 unit에 남습니다.

secret 정본은 `stack-secrets/hosts/<host>/valkey/`와 각 host manifest입니다.
서버 TLS·seed·ACL·복제·health 파일은 UID 999가 읽고, operator password는 호스트 root 전용입니다.
앱은 자기 `valkey-api-password` 또는 `valkey-worker-password`와 공통 `valkey-discovery-password`만 개별 mount합니다.
새 leaf는 기존 내부 CA로 host tailnet IP 및 health의 `127.0.0.1` SAN을 포함해 발급합니다.
개인키·전체 env·인증 암호를 출력하거나 CA 개인키를 host로 전송하지 않습니다.

`iris-stack/deploy/valkey-ha/`는 중앙의 반대 그룹 replica, kt032의 Hololive replica,
kapu의 두 replica와 세 Sentinel을 관리합니다. 초기 standalone stage에는 `sentinel` profile이 없습니다.
복제 수용 뒤 seed 정본·native runtime의 replica write gate를 1/5초로 맞추고 `CONFIG REWRITE`한 다음
host profile에 `sentinel`을 추가합니다. 준비 중에는 unit을 enable하지 않으며
최초 기동 시 `--wait`/PING을 full-sync·lineage·offset·TTL 보존의 대체 증거로 사용하지 않습니다.

기존 메모리-only 원본은 writer 정지와 승인된 임시 replication donor를 거쳐 영속 volume으로 옮깁니다.
원본→donor의 기존 plaintext/auth 경로와 donor→영속 노드의 TLS/replication-auth 경로를 구분합니다.
앱 재개 뒤에는 과거 donor·빈 AOF·이전 비영속 정의로 데이터 rollback하지 않습니다.
최신 데이터와 호환되는 image/profile/transport로 복구하며 기존 PostgreSQL 포함 unit은 유지합니다.
비동기 복제·AOF everysec·min-replicas는 무손실 동기 커밋이나 외부 부수효과 fencing이 아닙니다.
정기 백업 중단 정책은 변경하지 않습니다.

## Runtime Env Inputs

Central Quadlet units read `/etc/stack-secrets/hololive-bot/compose.env` through
`[Service] EnvironmentFile=`; `quadlet-prestart` rejects syntax that Compose and systemd
would read differently and moves passwords/API keys into root-only `--env-file`s.
The iris-seoul collector b unit has no interpolation file; its fixed values live in the unit and its
secrets only in `youtube-collector.env`. The legacy monolithic `env` file is not a production env file.

Application-only env is scoped per service. Quadlet units use the fixed files below:

```text
/etc/stack-secrets/hololive-bot/bot.env
/etc/stack-secrets/hololive-bot/alarm-worker.env
/etc/stack-secrets/hololive-bot/youtube-collector.env
```

### Central endpoint ownership

Collector b on iris-seoul names the central primary in its unit (`POSTGRES_HOST=100.100.1.8`, `POSTGRES_PORT=5433`)
and reads the dedicated scraper password from `youtube-collector.env` (`POSTGRES_PASSWORD`). It has no cache mounts
or DB/cache dependencies. The non-secret primary address owner is `deploy/topology/hosts.conf`
(`central=100.100.1.8`). Native generation reads that owner and `scripts/ci/postgres-capacity-policy.tsv`,
not a second address/pool literal. CLIProxy workstation addresses are unrelated to the primary DB route.

Collectors use only `youtube-collector.env`, so Iris egress tokens stay out of collector containers.
The central unsuffixed `youtube-collector` (`c`) also uses scoped
`youtube-collector.env`; it still must not receive Iris egress tokens, and `compose.env`
is only a `[Service]` interpolation file, never a container env file.

`youtube-collector.env` must provide a non-empty `METRICS_API_KEY` for every
deployed slot. Host-native `a`/`d` bind metrics to their tailnet address, so
omitting `METRICS_API_KEY` is a startup configuration error rather than a
keyless metrics mode.

Admin/LLM loopback bindings remain unchanged.
`HOLOLIVE_ADMIN_API_PORT_BIND_IP` must match `tailscale ip --assert` on the deployment
host before any build/start/restart. The approved current IPv4 binding remains
`100.100.1.8:30006/udp`. Admin sources are loopback and the Iris Console host (iris-seoul) `100.100.1.5/32`;
broad private CIDRs and wildcard source overrides fail preflight.

AP collectors no longer use Valkey (see the collector cache-topology contract).
The obsolete tailnet 6379 publish is removed; central private Unix socket, internal
network/password authentication and loopback remain. PostgreSQL AP access retains
TLS with `verify-full`/SCRAM. Role-specific HBA rules remain restrictive even when
container NAT presents AP traffic as a bridge gateway; source-IP rules alone are not isolation.
The tracked ingress nftables policy filters both host input and Podman (netavark) DNAT
forward paths for admin UDP 30006 and PostgreSQL TCP 5433. Mutation preflight requires
the reviewed installed file, active firewall unit, and loaded DB source/forward rules.
An empty source set, stale unit, or unavailable firewall inspection stops cutover.
Applying the firewall or unit/Compose changes requires the
separately authorized service deployment workflow; source validation activates none of them.

## Prepared database policy (not activated)

이 절의 연결 수치는 `max_connections=61`·exporter 2로 바꾼 2026-10-25 이전 준비안 기록입니다. 현재 서버 한도와 연결 예산은
[hololive-api runbook](../../docs/current/runbooks/hololive-api.md#postgresql-connections-pg18)과 `scripts/ci/postgres-capacity-policy.tsv`를 따릅니다.

PostgreSQL remains `max_connections=60`, with three superuser slots. The old application/
migrator allocation 55 plus exporter 1 and nightly 2 was 58 against 57 ordinary slots.
The local uniform collector maximum 7 (minimum 2 unchanged) gives runtime 22 + scraper 28
+ migrator 1 + exporter 1 + nightly 2 = 54, leaving **three ordinary control slots**.
Both capacity readers account for external consumers and aggregate all pools by role;
native max is emitted from the same policy. Role limits are runtime 25 (including bounded
maintenance control work), scraper 28, migrator 1, exporter 1 and nightly 2.
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
`PGSSLMODE=disable` and `Network=none`. Only DB/migrator mount the private tmpfs volume;
ordinary runtime containers never do. Operator/health/nightly `podman exec` sockets remain
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
the retained standby Quadlet reference unit is not authorization to enable replication access.

Server command flags prepare TCP keepalive 60/10/6, lock waits, all temp files, slow statements
at 1s, and bind-parameter lengths 0 for normal/error logging. SQL literals/comments can still
be sensitive; verify synthetic password/payload non-disclosure, retention and access controls.
No global runtime lock timeout is introduced. Existing WAL settings are explicitly encoded:
checkpoint 900s, max WAL 2GB, compression lz4, also in the inactive standby reference.
Persisted `postgresql.auto.conf` cleanup is a **separately approved operation**: capture old
values/sources and rollback flags, remove only the three approved entries (never RESET ALL),
then verify `source='command line'` and `pending_restart` after an approved `holo-postgres.service` restart.

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
`compose.env` (central) plus the per-service env files to the target host.

`scripts/deploy/lib/postgres-capacity.sh`는 host env의 PostgreSQL pool override key만 읽고 stack 전체 connection
budget을 다시 계산하는 공통 gate입니다. Target allocation이 `max_connections=61`에서 superuser 예약
3(`@superuser-reserved`)을 뺀 비슈퍼유저 슬롯 대비 최소 3개 control reserve를 남기지 않으면 진행하지 않습니다. 이 배포-host preflight는 Bash와 전송된 policy만 사용하며 CI용 uv/Python runtime을 요구하지 않습니다.
중앙은 이 gate를 host `compose.env`에 대해 실행합니다. `sync-opt-current.sh`는 unit 설치 전에, `quadlet-redeploy-service.sh`는
API·worker 교체 preflight에서 실행하며 실패하면 아무것도 바꾸지 않습니다. 중앙 unit의 기본 pool 값은
`scripts/ci/check-postgres-capacity.sh`가 `deploy/quadlet/hololive-bot`을 원본으로 확인하고, 같은 lib로 policy 기본값의
연결 예산을 계산합니다. host env 값은 위 배포 preflight만 확인합니다. 이 검사는 다른 env 값이나 secret을 출력하지 않습니다.

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

Local acceptance (not run by this preparation): `bash scripts/ci/check-postgres-capacity.sh` and
`bash scripts/deploy/test-postgres-access-policy.sh`. The latter requires the already-built
repository PG18.6 image (`POSTGRES_POLICY_TEST_IMAGE` may select its tag), rootless Podman and OpenSSL;
it uses a disposable internal network, tmpfs database/socket and synthetic certificates only,
publishes no ports, and proves HBA rejection, runtime/exporter TLS, migration/nightly socket
access and aggregate role limits. It never reads runtime env files or pulls/builds images.

## Requirements

- 중앙·iris-seoul: rootful Podman 5.x와 Quadlet. unit 원본은 `QUADLET_UNIT_DIRS=<dir> /usr/libexec/podman/quadlet -dryrun`을 통과해야 한다.
- 이미지 빌드는 kapu의 `podman build --format docker`(`./build-all.sh`)다. arm64 대상은 등록된 qemu-aarch64 binfmt가 필요하다.
  Dockerfile의 cache mount와 per-Dockerfile `.dockerignore`를 그대로 쓴다.
- production Go build는 `GOWORK=off`로 각 `go.mod`의 stable published external pin만 사용한다.
  로컬 sibling checkout은 image source가 아니다.
- 호스트 호환성 확인: 위 `quadlet -dryrun`이 에러 없이 끝나는지로 검증한다.
