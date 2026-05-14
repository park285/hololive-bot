# Scripts

루트 `scripts/`는 현재 7개 영역만 운영합니다.

## 1. ci/
로컬 CI gate 진입점입니다. `build-all.sh`는 Docker build 전에 이 gate를 실행합니다.

- `./scripts/ci/local-ci.sh`

기본 gate는 architecture gates, Go toolchain pin, `go work sync` drift, `gofmt`, `go fix` drift, `go mod tidy -diff`, `go vet`, `staticcheck`, `go build`, `go test -count=1`, `govulncheck`를 포함합니다. PostgreSQL integration test는 `TEST_DATABASE_URL`, race detector는 `RUN_RACE_TESTS=true`로 추가 실행합니다.

## 2. architecture/
PR/릴리스 전 경계 게이트와 릴리스 노트 렌더링 도구입니다.

- 표준 진입점: `./scripts/architecture/ci-boundary-gate.sh`
- 세부 체크:
  - `check-shared-go-boundary.sh`
  - `check-shared-go-packages.sh`
  - `check-go-compat-adapters.sh`
  - `export-go-workspace-import-graph.sh`
  - `check-go-alarm-contracts.sh`
  - `check-go-trigger-route-hardcoding.sh`
  - `check-go-module-loc.sh`
  - `check-deprecated-deadline.sh`
  - `check-release-governance-assets.sh`

## 3. deploy/
Docker Compose 운영 재배포 스크립트입니다.

- `./scripts/deploy/compose-redeploy-service.sh <service>`

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
- `./scripts/logs/osaka-status.sh`
- `./scripts/logs/osaka-logs.sh [youtube-scraper|stream-ingester|all]`
- `./scripts/logs/osaka-smoke.sh`
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
- `./scripts/smoke/smoke-dispatcher-ready.sh`
- `./scripts/smoke/smoke-runtime-health.sh`

정리 원칙:
- retired/no-op 스크립트는 유지하지 않습니다.
- 운영 표준 진입점은 README와 runbook에 문서화된 것만 남깁니다.
