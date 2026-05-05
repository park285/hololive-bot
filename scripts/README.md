# Scripts

루트 `scripts/`는 현재 4개 영역만 운영합니다.

## 1. architecture/
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

## 2. deploy/
Docker Compose 운영 재배포 스크립트입니다.

- `./scripts/deploy/compose-redeploy-service.sh <service>`

## 3. logs/
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
- `./scripts/logs/osaka-install-log-rollup.sh`

## 4. review/
리뷰 전달용 source/full bundle export와 사후 검증 스크립트입니다.

- `./scripts/review/export-source-bundle.sh [output_dir]`
- `./scripts/review/export-full-bundle.sh [output_dir]`
- `INCLUDE_UNTRACKED=true ./scripts/review/export-full-bundle.sh [output_dir]`
- `./scripts/review/verify-full-bundle.sh <bundle.tar.gz>`

정리 원칙:
- retired/no-op 스크립트는 유지하지 않습니다.
- 운영 표준 진입점은 README와 runbook에 문서화된 것만 남깁니다.
