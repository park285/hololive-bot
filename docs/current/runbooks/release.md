# Runbook: 릴리즈

## 역할

Runtime, document, and contract changes의 release checklist입니다.

## 버전 관리

저장소 전체 app 릴리즈 버전의 단일 기준은 루트 `VERSION`입니다. 안정 SemVer
`MAJOR.MINOR.PATCH` 형식으로 관리하고 Git tag에는 `v` 접두사를 붙입니다.
`hololive/hololive-api/VERSION`과 `hololive/hololive-alarm-worker/VERSION`은 각 container
artifact 버전이며 독립 build가 필요하면 서로 달라질 수 있습니다. 저장소 tag는 root
`VERSION`이 가리키는 전체 source snapshot에만 붙입니다.

collector·PO image도 `hololive/hololive-api/VERSION`을 쓰므로 API를 재빌드하지 않는 collector·PO
릴리스에서도 이 파일이 올라갈 수 있습니다. 이때 hololive-api image는 이전 build로 남지만, 중앙의
`hololive-api.service`가 (재)시작될 때마다(재부팅 포함) Quadlet prestart가 배포 트리의 이 파일 값을
`APP_VERSION`으로 넘깁니다. API가 보고하는 `APP_VERSION`은 repo 릴리스와 같아지지만 image의 revision·version
label은 이전 build를 가리킵니다. API source가 바뀌지 않은 릴리스에서만 생기는 식별자 차이이며, API 산출물 판정은
image label로 합니다.

릴리즈할 때는 다음 순서를 지킵니다.

1. 루트 `VERSION`을 변경하고 각 runtime artifact를 함께 릴리즈한다면 해당 runtime의
   `VERSION`도 변경합니다.
2. `CHANGELOG.md`의 `미출시` 항목을 `## v<version> - YYYY-MM-DD` 구간으로 옮깁니다.
3. `bash scripts/check-release-version.sh`와 전체 pre-push gate를 통과시킵니다.
4. 검증된 commit을 `main`에 반영한 뒤 annotated tag `v<version>`을 별도 승인된 publish
   절차로 push합니다.

`MAJOR`는 호환되지 않는 공개·운영·데이터 계약 변경, `MINOR`는 하위 호환 기능 추가,
`PATCH`는 하위 호환 수정과 보안·운영 안정화에 사용합니다. 버전 관리 도입 전 이력은
`CHANGELOG.md`의 실제 날짜·commit SHA 기준점을 유지하며 추정 버전을 소급하지 않습니다.

### 11.0.0 worker/DB cutover

먼저 구 API/alarm settings·mode를 받는 **10.2.1 호환 baseline을 실제 운영에서 수용**합니다.
구 프로필 CLI·인증된 loaded hash·atomic source drift·paired rollback 증거 없이 새 settings를
활성화하지 않습니다. 준비 receipt나 source 통합은 이 수용을 대신하지 않습니다.
[worker settings cutover 절차](../../../deploy/compose/README.md#worker-settings-cutover-1100)는
11.0.0의 단일 strict schema, 최초 capture, no-build 후보 검사와 복구 bundle을 정의합니다.
다섯 genuine shared-go v2.14.0 pin/sum과 전체 upstream migration manifest 324까지를 유지하며
stream-duration·time-finalization·membership·Node·issuer 산출물을 함께 보존합니다.
이 worker/DB 운영 변경에 data migration을 추가하거나 구 321 manifest를 실행하지 않습니다.

11.0.0의 API/alarm·collector pair와 standalone Seoul 구성을 검증한 뒤 승인된 순서로 교체합니다.
privileged migrator는 전용 Unix socket·network-none이고 network consumer는 TLS verify-full입니다.
role limit SQL은 배포 wrapper가 실행하지 않습니다. rollout 소유자가 collector 네 곳 모두 max 7
수용을 확인한 **뒤에만** 승인된 admin socket으로 적용합니다. HBA/WAL/server 재생성과
persisted 설정 정리도 각 승인·증거를 남깁니다. Valkey noeviction은 별도 선행 관측이 없으면
활성화하지 않습니다. 이 문서는 새 운영 적용 증거를 주장하지 않습니다.


## Compose service 재배포

빌드 호스트와 중앙 런타임 호스트는 서로 다른 머신입니다. 중앙 런타임 호스트에서는
어떤 빌드도 돌리지 않습니다 — 이미지는 빌드 호스트에서 만들어 전송하고, 런타임
호스트는 이미 적재된 이미지로 recreate만 합니다.

모든 build·deploy·restart는 `hololive-bot-ops`로 라우팅하고 각각 필요한 승인을
확인합니다. 검증된 clean revision의 전체 image build-only 진입점은
`./build-all.sh --no-bump --build-only`(`podman build`)입니다. 중앙 service의 실제 배포는 아래의
build host 생성·검증, image와 deploy tree 전송, runtime host Quadlet 기동 세 단계를 분리해 수행합니다.
중앙 unit 정의와 설치는 `deploy/quadlet/README.md`가 소유합니다.

### 1. 빌드 호스트: 이미지 생성과 검증

검토된 revision에서 clean worktree로 빌드하고, 세 항목이 모두 맞는지 확인합니다. x86_64 build host의
arm64 빌드는 등록된 qemu-aarch64 binfmt가 있어야 하며 없으면 `build-all.sh`가 빌드 전에 멈춥니다.

```bash
HOLOLIVE_BUILD_PLATFORM=linux/arm64 ./build-all.sh --no-bump --build-only

podman image inspect localhost/<image>:prod \
  --format '{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.version"}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
```

`arch=arm64`가 아니거나 revision이 `unknown`·short SHA이면 중단합니다.

### 2. 전송

image는 build host에서 `:candidate` 이름으로 저장해 중앙에 적재합니다. 중앙의 `:prod` tag는 옮기지 않습니다.
API·worker의 실행 image는 worker-profile drop-in이, infra image는 host `compose.env`의 identity가 정합니다
([image identity 소유](../../../deploy/quadlet/README.md#이미지)). collector c와 PO issuer는 kapu의
`scripts/deploy/po-central-cutover.sh`가 build·전송·교체를 함께 소유합니다.

```bash
podman tag localhost/<image>:prod localhost/<image>:candidate
podman image inspect localhost/<image>:candidate --format 'sha256:{{.Id}}'    # build host identity를 기록
podman save --format docker-archive localhost/<image>:candidate | gzip -1 \
  | ssh <central-host> 'gunzip | sudo -n podman load'
ssh <central-host> "sudo -n podman image inspect localhost/<image>:candidate --format 'sha256:{{.Id}}'"
```

`build-all.sh`는 `--format docker`로 빌드합니다. `oci-archive`로 옮기면 config가 OCI 형식으로 바뀌어
수신측 `.Id`가 build host와 달라지므로 `docker-archive`를 씁니다(2026-11-04 리허설 VM 실측). 두 identity가 다르면
배포하지 않습니다.

rollback 기준은 다음과 같습니다. API·worker는 배포 helper가 실행 중이던 image·profile pair를 release 디렉터리와
`hololive-worker-profile:pair-*` tag로 보존합니다. infra image는 바꾸기 전 `compose.env` 값(이전 identity)과 그 image이며,
수용 전에는 이전 image를 지우지 않습니다. 직전 배포 트리(`/opt/hololive-bot/compose/current.bak-*`)도 함께 보존합니다.

### 3. 런타임 호스트: Quadlet 기동

배포할 commit의 clean checkout에서 `sudo ./scripts/deploy/sync-opt-current.sh`로 `/opt/hololive-bot/compose/current`와
unit을 갱신합니다. 이 도구는 배포 트리에 그 commit을 `.source-revision`으로 남기고, host `compose.env`의 PostgreSQL
연결 예산을 확인하지 못하면 아무것도 설치하지 않습니다. unit은 빌드하지 않으며(`Pull=never`), prestart가 bind 주소·비밀
파일·방화벽을 확인합니다.
기존 runtime data/config/권한 변경을 승인하지 않은 갱신은 `--preserve-runtime`을 명시합니다.
새 고유 exec-tree backup만 만들며 과거 backup을 덮거나 자동 정리하지 않습니다.

API·worker는 배포 트리의 배포 helper로 교체합니다. helper가 이전 pair 보존, candidate 검사, 활성화 전
`hololive-db-migrate`(같은 revision의 API image로 고정), drop-in 활성화와 restart, `verify-live`·`NRestarts` 확인을 한
번에 합니다. 활성화 뒤 실패해도 자동으로 이전 pair를 복원하지 않습니다. 따로 `systemctl start hololive-db-migrate.service`나
`systemctl restart`를 실행하지 않습니다. 단계와 결과 줄은 [배포 helper](../../../deploy/quadlet/README.md#배포-helper)에 있습니다.

```bash
helper=/opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh
sudo "$helper" hololive-api sha256:<api-id>
sudo "$helper" hololive-alarm-worker sha256:<worker-id> sha256:<api-id>
```

Iris와 인증 계약을 함께 전환할 때는 위 단일 서비스 호출 대신
[동시 전환의 `--quiesce`/`--resume`](../../../deploy/quadlet/README.md#iris-소비자-동시-전환)을 사용합니다.
정지 완료와 배포 완료를 구분하고 두 후보 drop-in을 모두 설치하기 전에는 peer를 기동하지 않습니다.

마지막 줄이 `[OUTCOME] deployed`가 아니면 실패입니다. `not_activated`는 실행 중 pair를 바꾸지 않았다는 뜻입니다(migration은
적용됐을 수 있음). `failed_after_activation`은 candidate가 활성화된 채 남았다는 뜻이며 helper는 자동으로 되돌리지 않습니다.
release 디렉터리를 보존하고, 스키마·admission 호환성을 승인받은 뒤에만 [rollback 런북](rollback.md)의 `--check`·`--apply`로
이전 pair를 되살립니다. 호환되지 않으면 fix-forward합니다. helper는 실패한 단계를 재시도하지 않습니다.

런타임 peer는 `Wants=`라 한 unit의 restart가 다른 unit을 재시작하지 않습니다. 다만 멈춰 있는 peer는 함께 시작되므로
`hololive-api.service`를 시작하면 `hololive-alarm-worker.service`도 시작됩니다. main-host collector-c는
`hololive-youtube-collector-c.service`이며 `po-central-cutover.sh`가 PO issuer를 먼저 교체·검증한 뒤 collector를
재시작합니다. infra image는 `compose.env`의 identity를 바꾼 뒤 승인된 창에서 해당 unit만 재시작합니다.

**컷오버 직후**에는 app unit이 아직 drop-in 없이 `:prod`·compose.env 값을 따르므로, 첫 배포 전에
[첫 고정](../../../deploy/quadlet/README.md#컷오버-직후-첫-고정) 절차로 세 app 서비스의 drop-in을 만듭니다.

### 4. 수용 증거

변경 시작 시각을 기록해 두고 다음을 확인합니다.

- helper의 `[OUTCOME] deployed` 줄(서비스, revision, image identity, release 디렉터리)
- 대상 container의 `StartedAt`이 변경 시작 이후이고 health가 `healthy`
- 실행 중 image identity(`sha256:` + `podman inspect --format '{{.Image}}' <container>`)와 revision label이 검토된 SHA와 일치
- `systemctl show <unit>.service -p NRestarts --value`가 `0`. 명시적 restart가 0으로 되돌리므로 이후 값은 자동
  재시작(unhealthy 정지·crash) 횟수입니다. Podman은 기동마다 새 container(`--replace --rm`)라 container `RestartCount`는
  늘 0이어서 기준으로 쓰지 않습니다.

건강한 container가 다른 image ID로 돌고 있으면 실패한 rollout입니다. 이전 pair release 디렉터리와 직전 배포 트리는
수용될 때까지 보존합니다. PO issuer(`youtube-po-b`, `youtube-po-c`, native `hololive-youtube-po.service`)의 재시작
카운트는 generation 교체 횟수이므로 `NRestarts=0` 대신 [youtube-collector 런북의 issuer 수용 기준](youtube-collector.md#issuer-generation-교체와-재시작-카운트)을 따릅니다.

> 중앙 Postgres publish 주소·인증서 mount는 `holo-postgres.container`와 app unit이 정의하고 기동 전에 `quadlet-prestart`가
> topology 정본·Tailscale identity와 대조한다. Docker 시절 live-compat overlay와 그 cert mount 검사는 컷오버로 없어졌다.

Current Go runtime services:

- `hololive-api`
- `hololive-alarm-worker`
- `youtube-collector` on the main host; `youtube-collector-b` uses the AP host wrapper.

## 필수 검사

```bash
./scripts/architecture/ci-boundary-gate.sh
```

## PostgreSQL hot path plan gate

Before releasing PostgreSQL GUC, migration, alarm dispatch, or YouTube outbox claim changes, capture fresh plan snapshots from the target environment:

```bash
DATABASE_URL=... ./scripts/runtime/pg-hotpath-explain-snapshot.sh \
  --stats-window-seconds 60 \
  --output-dir artifacts/pg-hotpath-explain/$(date -u +%Y%m%dT%H%M%SZ)
```

The script accepts `DATABASE_URL` or standard `PG*` variables and does not embed secrets. The YouTube claim snapshot mirrors the current runtime defaults (`LockTimeout=5m`, `ClaimFreshnessWindow=2h`). It writes:

- `invalid-indexes.txt`
- `target-indexes.txt`
- `dead-tuples-autovacuum.txt`
- `claim-statement-window.txt`
- `alarm-dispatch-claim-explain.txt`
- `youtube-outbox-claim-explain.txt`

Required index contracts:

- `alarm_dispatch_deliveries` claim: `idx_alarm_dispatch_deliveries_due`,
  `idx_alarm_dispatch_deliveries_send_unit`,
  `idx_alarm_dispatch_deliveries_send_unit_due`
- `youtube_notification_outbox` claim: `idx_yno_pending_due_created_id`

The script requires the PostgreSQL 18 `pg_stat_statements` schema. It verifies that the required indexes exist, are ready and valid, and match the required table, key order, and partial predicate. PostgreSQL may choose another valid index for a small relation, so plan selection is recorded separately from the catalog contract. `claim-statement-window.txt` takes ordered `pg_stat_statements` snapshots around a bounded observation interval (60 seconds by default), joins exact `(dbid, userid, queryid, toplevel)` identities, and evaluates only the interval deltas for `calls` and `total_exec_time`. Claim classification fingerprints the runtime CTE, `UPDATE`, and `RETURNING` structure from `claim_due.sql` and `fanout_claim.sql`; alarm maintenance claims and the YouTube revive query are not performance evidence. Ambiguous fingerprints are ignored. The observer SQL excludes its own `hololive-pg-hotpath-stats-observer` marker explicitly. Lifetime means do not affect the gate.

The artifact retains the complete whitespace-normalized `pg_stat_statements` representative SQL for those two static runtime claims so the fingerprint can be revalidated; it does not truncate the query text. Bind values remain parameter placeholders and the owned claim SQL contains no dynamic comments or identifiers, so application values and credentials are not expected in this artifact. It still exposes internal table and column names: keep it as internal operational evidence, review it before external sharing, and never place secrets in SQL comments or identifiers.

The stats window is fresh only when at least one `alarm_dispatch_deliveries` claim and one `youtube_notification_outbox` claim complete during the interval. No matching call for either required hot path, a global `pg_stat_statements` reset, entry deallocation, a per-statement `stats_since` change, or a decreasing counter makes the result inconclusive and fails the gate. This also rejects a statement reset whose counters recover past the starting values before the second snapshot. Rerun during a representative active window instead of treating missing evidence as a pass. The script also fails when the fresh delta mean exceeds 5ms, any index contract is broken, any invalid index exists, or `Rows Removed by Filter` exceeds 1000. The dead-tuple snapshot records heap, TOAST, and index statistics for a fixed hot-path table list and remains review evidence rather than a fixed threshold. The EXPLAIN statements run in a transaction and end with `ROLLBACK`, but they still use `ANALYZE`; run them during a low-risk verification window.

## 계약 변경 릴리즈 규칙

- Keep provider and consumer compatible during rollout.
- Use dual-read/dual-write or additive fields for queue/envelope changes.
- Update `CONTRACT_MAP.md`, matching `contracts/*.md`, and runbook impacts before release.
- Include rollback notes for old and new contract versions.

## 릴리즈 노트

GitHub Release 제목은 tag와 같은 `v<version>`으로 쓰고, 본문은
[`v3.5.0`](https://github.com/park285/hololive-bot/releases/tag/v3.5.0)처럼 GitHub가
생성한 `What's Changed`, PR 제목·작성자·링크, `Full Changelog` 형식을 사용합니다.
수동 운영 요약이나 `CHANGELOG.md` 복사본으로 대체하지 않습니다.

`previous_tag`는 직전 게시 릴리즈로 명시하여, tag만 있고 Release가 없는 중간 버전의
변경도 누락하지 않습니다. 첫 게시 릴리즈는 직전 app tag를 기준으로 합니다.
게시·본문 수정의 단일 진입점은 `scripts/publish-release.sh`입니다. 기본 mode는
`preview`이며 대상 tag·직전 게시 릴리즈·자동 생성 본문을 확인합니다. GitHub API의
자동 생성이 실패하거나 결과가 비면 중단하며 수동 본문으로 대체하지 않습니다.
직전 게시 릴리즈와 다른 기준은 거부합니다. 첫 게시 릴리즈는 직전 app tag를
명시합니다. 기존 릴리즈 수정 시에는 해당 릴리즈의 게시 시각 이전을 기준으로 합니다.

```bash
bash scripts/publish-release.sh "$tag" "$previous_tag"
# 기존 검증과 main 반영, annotated tag push 및 게시 승인을 마친 뒤:
bash scripts/publish-release.sh "$tag" "$previous_tag" create
# 승인된 기존 본문 교정:
bash scripts/publish-release.sh "$tag" "$previous_tag" edit
```

스크립트는 tag를 생성하거나 push하지 않으며 전체 pre-push gate를 대신하지 않습니다.
`create`는 원격 annotated tag를 요구합니다. `edit`는 원문을 로컬 임시 디렉터리에
보존한 뒤 제목·본문만 수정하고 tag·첨부물·draft/prerelease 상태 보존을 확인합니다.
기록 경로는 실행 시 출력합니다. 생성 본문과 게시 결과가 다르면 실패로 보고하며
자동 재시도하지 않습니다. 임의 `gh release create/edit`나 웹 수동 요약으로 우회하지 않습니다.

배포·검증·롤백 근거는 `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md`와
`scripts/architecture/render-release-notes.sh`를 이용해 별도 운영 기록으로 유지합니다.
이 양식은 공개 GitHub Release 본문 생성기가 아닙니다.

## Smoke test

다음 확인은 이미지를 다시 빌드하거나 unit을 재시작하지 않습니다. 중앙 호스트에서 실행 중인 컨테이너를 대상으로 합니다.

```bash
sudo podman exec hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
sudo podman exec hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
sudo podman exec hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/health
sudo podman exec hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
sudo podman exec hololive-youtube-collector-c ./bin/healthcheck https://127.0.0.1:30025/health
```

## 관련 문서

- `../DEPLOYMENT_BASELINE.md`
- `pgo.md`
- `rollback.md`
