# Runbook: 릴리즈

## 역할

Runtime, document, and contract changes의 release checklist입니다.

## 버전 관리

저장소 전체 app 릴리즈 버전의 단일 기준은 루트 `VERSION`입니다. 안정 SemVer
`MAJOR.MINOR.PATCH` 형식으로 관리하고 Git tag에는 `v` 접두사를 붙입니다.
`hololive/hololive-api/VERSION`과 `hololive/hololive-alarm-worker/VERSION`은 각 container
artifact 버전이며 독립 build가 필요하면 서로 달라질 수 있습니다. 저장소 tag는 root
`VERSION`이 가리키는 전체 source snapshot에만 붙입니다.

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

## Compose service 재배포

빌드 호스트와 중앙 런타임 호스트는 서로 다른 머신입니다. 중앙 런타임 호스트에서는
어떤 빌드도 돌리지 않습니다 — 이미지는 빌드 호스트에서 만들어 전송하고, 런타임
호스트는 이미 적재된 이미지로 recreate만 합니다.

모든 build·deploy·restart는 `hololive-bot-ops`로 라우팅하고 각각 필요한 승인을
확인합니다. 검증된 clean revision의 전체 image build-only 진입점은
`./build-all.sh --no-bump --build-only`이고, 개별 중앙 service의 build와 cutover
진입점은 `./scripts/deploy/compose-redeploy-service.sh <service>`입니다.

`./scripts/deploy/compose-redeploy-service.sh`는 cutover 전에 `compose build`를
수행하므로 **빌드 호스트 전용**입니다. 런타임 호스트에서 실행하면 그 호스트에서
컴파일이 돌고, `/opt/hololive-bot/compose/current`에는 `.git`이 없어
`org.opencontainers.image.revision`이 `unknown`으로 찍힙니다.

### 1. 빌드 호스트: 이미지 생성과 검증

검토된 revision에서 clean worktree로 빌드하고, 세 항목이 모두 맞는지 확인합니다.

```bash
docker buildx build --builder <multiarch-builder> --platform linux/arm64 --load \
  -t <service>:prod-arm64 -f <service-dockerfile> \
  --build-arg VERSION="$(xargs <VERSION)" --build-arg REVISION="$(git rev-parse HEAD)" .

docker image inspect <service>:prod-arm64 \
  --format '{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.version"}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
```

`arch=arm64`가 아니거나 revision이 `unknown`·short SHA이면 중단합니다.

### 2. 롤백 보존, 전송과 승격

현재 `prod` image가 있으면 새 image를 승격하기 전에 UTC timestamp tag로 보존합니다.
이 tag와 직전 compose tree가 이번 cutover의 1차 rollback artifact입니다.

```bash
rollback_stamp="$(date -u +%Y%m%dT%H%M%SZ)"
ssh <central-host> "if sudo -n docker image inspect <service>:prod >/dev/null 2>&1; then \
  sudo -n docker tag <service>:prod <service>:rollback-${rollback_stamp}; fi"
```

기존 `prod` image가 없는 최초 배포일 때만 `image inspect` 실패를 허용합니다. 기존
image가 있는데 tag 생성이 실패한 경우에는 전송·승격을 진행하지 않습니다.

```bash
docker save <service>:prod-arm64 | gzip -1 \
  | ssh <central-host> 'gunzip | sudo -n docker load'
ssh <central-host> 'sudo -n docker tag <service>:prod-arm64 <service>:prod'
```

### 3. 런타임 호스트: no-build cutover

`/opt/hololive-bot/compose/current`에서 실행합니다. `compose.sh`는 `--build`를 주지
않으면 빌드하지 않으며, bind-mount 쓰기 권한·public ingress 렌더·health gate
preflight가 걸립니다. 부분 `up`에는 항상 `--no-deps`를 붙입니다 — `hololive-api`의
`depends_on`에 `hololive-alarm-worker`가 있어 생략하면 alarm dispatcher가 함께 뜹니다.

```bash
export COMPOSE_ENV_FILE=/etc/stack-secrets/hololive-bot/compose.env

# 마이그레이션 one-shot 먼저, 종료 코드 0 확인
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-deps hololive-db-migrate
docker wait hololive-db-migrate

./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  up -d --no-deps <service>...
```

main-host active-active AP는 main-ap overlay 2종과 profile을 함께 넣습니다:

```bash
COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh \
  -f deploy/compose/docker-compose.prod.yml \
  -f deploy/compose/docker-compose.live-compat.yml \
  -f deploy/compose/docker-compose.main-ap.yml \
  -f deploy/compose/docker-compose.main-ap.live-compat.yml \
  up -d --no-deps youtube-collector
```

### 4. 수용 증거

recreate 시작 시각을 기록해 두고, 대상 컨테이너의 `StartedAt`이 그 이후이며
health가 `healthy`, `RestartCount=0`, 실행 중 이미지의 revision이 검토된 SHA와
일치하는지 확인합니다. 건강한 컨테이너가 다른 이미지 ID로 돌고 있으면 실패한
rollout입니다. 롤백 태그와 직전 배포 트리는 수용될 때까지 보존합니다.

> 배포 스크립트는 기존 host-network Postgres 런타임에서 live-compat overlay 없이
> 배포하려는 경로를 fail-closed로 거부한다. live-compat overlay는 이제
> `network_mode: host` 대신 bridge network를 유지하고 `<tailnet-central>:5433`을
> container `5432`에 명시 bind한다. 의도적 토폴로지 변경 시에만
> `ALLOW_POSTGRES_TOPOLOGY_CHANGE=true`를 설정한다.

> Security (a691472f): live-compat overlay는 현 호스트의 PostgreSQL/cert
> 토폴로지를 반영하는 필수 overlay이며 "약화 레이어"가 아니다. 과거 이 overlay가
> 비-bot 서비스(admin-api 등)에 `/run/hololive-bot/certs` 디렉터리 전체를
> `!override` 마운트해 불필요한 cert를 노출하던 결함은 파일 단위 마운트로 환원해
> 해소했다. 단 `postgres-ca.pem`은 모든 app 서비스(admin-api 포함)가
> PostgreSQL에 `verify-full`로 연결하는 데 필수이므로 파일 단위로
> 함께 mount한다 — 디렉터리→파일 환원(0c8d4125) 시 이 파일이 누락돼 재배포 컨테이너가
> `unable to read CA file`로 크래시하던 회귀를 복원한 것이다(06-20 이전 컨테이너는 옛
> 디렉터리 mount 유지로 미발현). admin-api는 `hololive-h3.{crt,key}`·`iris-ca.pem`·
> `postgres-ca.pem`을 개별 마운트하고 디렉터리 전체는 더 이상 마운트하지
> 않는다(검증: `scripts/deploy/test-live-compat-cert-mount-scope.sh`).

Current Go runtime services:

- `hololive-api`
- `hololive-alarm-worker`
- `youtube-collector` on the main host; `youtube-collector-b` uses the AP host wrapper.

## 필수 검사

```bash
./scripts/architecture/ci-boundary-gate.sh
go test . -run TestRuntimeSplitStandaloneModulesContract
```

For contract/document changes:

```bash
./scripts/architecture/check-current-docs-no-historical.sh
./scripts/architecture/check-current-docs-no-historical-body.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-contract-map.sh
./scripts/architecture/check-internal-route-hardcoding.sh
./scripts/architecture/check-error-contracts.sh
./scripts/architecture/check-release-governance-assets.sh
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

The script requires the PostgreSQL 18 `pg_stat_statements` schema. It verifies that both indexes exist, are ready and valid, and match the required table, key order, and partial predicate. PostgreSQL may choose another valid index for a small relation, so plan selection is recorded separately from the catalog contract. `claim-statement-window.txt` takes ordered `pg_stat_statements` snapshots around a bounded observation interval (60 seconds by default), joins exact `(dbid, userid, queryid, toplevel)` identities, and evaluates only the interval deltas for `calls` and `total_exec_time`. Claim classification fingerprints the runtime CTE, `UPDATE`, and `RETURNING` structure from `repository_claim_0053_02.sql` and `dispatcher_claim_0050_01.sql`; alarm maintenance claims and the YouTube revive query are not performance evidence. Ambiguous fingerprints are ignored. The observer SQL excludes its own `hololive-pg-hotpath-stats-observer` marker explicitly. Lifetime means do not affect the gate.

The artifact retains the complete whitespace-normalized `pg_stat_statements` representative SQL for those two static runtime claims so the fingerprint can be revalidated; it does not truncate the query text. Bind values remain parameter placeholders and the owned claim SQL contains no dynamic comments or identifiers, so application values and credentials are not expected in this artifact. It still exposes internal table and column names: keep it as internal operational evidence, review it before external sharing, and never place secrets in SQL comments or identifiers.

The stats window is fresh only when at least one `alarm_dispatch_deliveries` claim and one `youtube_notification_outbox` claim complete during the interval. No matching call for either required hot path, a global `pg_stat_statements` reset, entry deallocation, a per-statement `stats_since` change, or a decreasing counter makes the result inconclusive and fails the gate. This also rejects a statement reset whose counters recover past the starting values before the second snapshot. Rerun during a representative active window instead of treating missing evidence as a pass. The script also fails when the fresh delta mean exceeds 5ms, any index contract is broken, any invalid index exists, or `Rows Removed by Filter` exceeds 1000. The dead-tuple snapshot remains review evidence rather than a fixed threshold. The EXPLAIN statements run in a transaction and end with `ROLLBACK`, but they still use `ANALYZE`; run them during a low-risk verification window.

## 계약 변경 릴리즈 규칙

- Keep provider and consumer compatible during rollout.
- Use dual-read/dual-write or additive fields for queue/envelope changes.
- Update `CONTRACT_MAP.md`, matching `contracts/*.md`, and runbook impacts before release.
- Include rollback notes for old and new contract versions.

## 릴리즈 노트

Use:

- `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md`

## Smoke test

These scripts do not rebuild, redeploy, or recreate Docker Compose services. `smoke-runtime-health.sh` expects local services to already be running.

```bash
./scripts/smoke/smoke-compose-config.sh
./scripts/smoke/smoke-runtime-health.sh
```

Equivalent manual checks:

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30001/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30006/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-alarm-worker ./bin/healthcheck https://127.0.0.1:30007/health
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml exec -T hololive-api ./bin/healthcheck https://127.0.0.1:30003/health
COMPOSE_PROFILES=main-ap ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.main-ap.yml exec -T youtube-collector ./bin/healthcheck https://127.0.0.1:30025/health
```

## 관련 문서

- `../DEPLOYMENT_BASELINE.md`
- `pgo.md`
- `rollback.md`
