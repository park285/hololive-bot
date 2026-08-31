# Scripts

루트 `scripts/`는 현재 11개 영역을 운영합니다: `ci/`, `architecture/`, `deploy/`, `logs/`, `review/`, `runtime/`, `smoke/`, `maintenance/`, `ops/`, `refactor/`, `systemd/`.

## 1. ci/
로컬 CI gate 진입점입니다. `build-all.sh`는 Docker build 전에 이 gate를 실행합니다.

- `./scripts/ci/local-ci.sh`

기본 gate는 architecture gates, Go toolchain pin, `go work sync` drift, `gofmt`, `go fix` drift, `go mod tidy -diff`, `go vet`, `staticcheck`, stage-3 `golangci-lint`, NilAway, `go build`, PGO-off production policy, `go test -count=1`, race detector, `govulncheck`를 포함합니다. PostgreSQL integration test는 `TEST_DATABASE_URL`이 설정된 경우 추가 실행합니다.

## 2. architecture/
PR/릴리스 전 경계 게이트와 릴리스 노트 렌더링 도구입니다.

- 표준 진입점: `./scripts/architecture/ci-boundary-gate.sh`
- 세부 체크:
  - `check-shared-go-boundary.sh`
  - `check-shared-go-packages.sh`
  - `check-go-compat-adapters.sh`
  - `check-go-generic-internal-package-names.sh`
  - `export-go-workspace-import-graph.sh`
  - `check-current-docs-root-allowlist.sh`
  - `check-go-toolchain-parity.sh`
  - `check-go-alarm-contracts.sh`
  - `check-go-trigger-route-hardcoding.sh`
  - `../ci/check-structure.sh`
  - `check-deprecated-deadline.sh`
  - `check-release-governance-assets.sh`

## 3. deploy/
Docker Compose 운영 재배포 스크립트입니다.

- `./scripts/deploy/compose-redeploy-service.sh <service>`
- 원격 AP(`youtube-collector` fleet) 호스트 운영: `scripts/deploy/ap-hosts/<host>.conf` 기반
  - Seoul Compose: `./scripts/deploy/ap-deploy.sh seoul [--dry-run|--apply]`
  - Osaka/Osaka2 native: `./scripts/deploy/ap-host-native-deploy.sh <osaka|osaka2> [--dry-run|--apply]`
  - `./scripts/deploy/ap-completion-check.sh <host>`
  - Seoul Compose rollback: `./scripts/deploy/ap-rollback.sh seoul [--dry-run|--apply]`
  - Osaka/Osaka2 native rollback: `./scripts/deploy/ap-host-native-rollback.sh <osaka|osaka2> [--dry-run|--apply]`
  - `./scripts/deploy/ap-iris-h3-trust-preflight.sh <host>`

## 4. logs/
Compose 로그 조회/테일/보조 미러링 단일 진입점입니다.

- `./scripts/logs/logs.sh query <service>`
- `./scripts/logs/logs.sh tail <service>`
- `ENABLE_LOG_AUX_FILES=1 ./scripts/logs/logs.sh backfill <service>`
- `ENABLE_LOG_MIRROR=1 ./scripts/logs/logs.sh stream start`
- `ENABLE_LOG_MIRROR=1 ./scripts/logs/logs.sh dump`
- `./scripts/logs/logs.sh prune`
- `./scripts/logs/logs.sh canary`
- `ENABLE_LOG_AUX_FILES=1 ./scripts/logs/logs.sh canary-cron`
- `./scripts/logs/ap-status.sh <host>` (osaka, seoul)
- `./scripts/logs/ap-logs.sh <host> [youtube-collector|all]`
- `./scripts/logs/ap-smoke.sh <host>`
- `./scripts/logs/osaka-install-log-rollup.sh` - legacy Osaka log rollup timer를 masked 상태로 유지합니다.

## 5. review/
리뷰 전달용 source/full bundle export와 사후 검증 스크립트입니다.

- `./scripts/review/export-source-bundle.sh [output_dir]`
- `./scripts/review/export-full-bundle.sh [output_dir]`
- `INCLUDE_UNTRACKED=true ./scripts/review/export-full-bundle.sh [output_dir]`
- `./scripts/review/verify-full-bundle.sh <bundle.tar.gz>`

## 6. runtime/
운영 중 런타임 상태 조회와 안전한 보정 작업용 스크립트입니다.

- `./scripts/runtime/alarm-dispatch-outbox-status.sh`
- `./scripts/runtime/alarm-dispatch-outbox-requeue.sh`
- `./scripts/runtime/alarm-dispatch-outbox-retention.sh`
- `./scripts/runtime/requeue-alarm-dlq.sh`
- `./scripts/runtime/set-iris-base-url.sh`

## 7. smoke/
Compose 설정과 런타임 readiness/health smoke test 스크립트입니다.

- `./scripts/smoke/smoke-compose-config.sh`
- `./scripts/smoke/smoke-runtime-health.sh`

## 8. maintenance/
수동 적용용 PostgreSQL 유지보수 SQL입니다.

- `hololive_msa_hot_path_observability.sql`
- `pg18_db_usage_optional_concurrent_indexes.sql`

## 9. ops/
Valkey self-heal 및 PostgreSQL failover 운영 자산입니다.

- `./scripts/ops/valkey-selfheal.sh` (+ `valkey-selfheal.service`/`.timer`, `valkey-selfheal_test.sh`)
- `./scripts/ops/postgres-failover.sh` (+ non-root launcher, `.service`/`.timer`, apply/env examples, `postgres-failover_test.sh`)
- `./scripts/ops/postgres-failover-fence-ssh.sh` - reachable primary용 reference fence hook
- `./scripts/ops/postgres-primary-fence.sh` - 구 primary에서 compose/DB 재기동을 영속 차단하는 remote action
- `./scripts/ops/postgres-primary-unfence.sh` - 재시딩·streaming 검증 뒤 fence generation을 안전하게 해제하는 root helper
- `./scripts/maintenance/postgres-failover-db-role.sql` - controller DB role에 CONNECT와 `pg_promote()`만 부여하는 수동 bootstrap

PostgreSQL controller는 Docker 권한 없는 `hololive-pg-failover` 사용자로 실행되고 checked-in
unit은 dry-run입니다. 실제 승격은 승인된 external fencing과
route hook을 모두 준비한 뒤 apply drop-in을 설치해야 합니다. 운영 계약은
`docs/current/runbooks/postgres-replication.md`가 소유합니다.

## 10. refactor/
리팩터링 보조 가드 스크립트입니다.

- `./scripts/refactor/grep-sensitive-logs.sh`
- `./scripts/refactor/validate-no-admin-touch.sh`

## 11. systemd/
호스트 배포용 systemd unit 정본입니다.

- `hololive-compose.service` (+ `hololive-compose.service.d/`)
- `hololive-daily-log-rollup.service`/`.timer`, `hololive-osaka-daily-log-rollup.service`/`.timer`
- `hololive-main-log-mirror@.service`/`.timer`
- `admin-dashboard-ingress`는 Compose의 host-networked Nginx 서비스로 `100.100.1.8:30191`을 수신하고 `127.0.0.1:30190`으로 전달합니다.

정리 원칙:
- retired/no-op 스크립트는 유지하지 않습니다.
- 운영 표준 진입점은 README와 runbook에 문서화된 것만 남깁니다.
