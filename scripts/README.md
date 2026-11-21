# Scripts

루트 `scripts/`는 현재 12개 영역을 운영합니다: `ci/`, `architecture/`, `build/`, `deploy/`, `experiments/`, `logs/`, `review/`, `runtime/`, `maintenance/`, `perf/`, `refactor/`, `systemd/`.

GitHub Release 게시·본문 수정: `bash scripts/publish-release.sh <tag> <previous-tag> [preview|create|edit]`. 기본은 미리보기이며 [릴리즈 runbook](../docs/current/runbooks/release.md)의 승인·검증 절차를 따릅니다.

## 1. ci/
로컬 CI gate 진입점입니다. `build-all.sh`는 image build(`podman build`) 전에 이 gate를 실행합니다.

- `./scripts/ci/local-ci.sh`

기본 게이트는 architecture gates, `go work sync` drift, 모듈별 `GOWORK=off` tidy·vet, stage-3 golangci-lint(staticcheck·formatter·modernize), NilAway, Go build, collector production 빌드와 race 시험을 포함합니다. `GOTOOLCHAIN` 기본값은 `auto`이며 명시한 값은 유지합니다. 비경쟁 전용 시험과 게시 전 범위 판정은 [CI 게이트](../docs/current/architecture/ci-gates.md)와 [게시 절차](../docs/current/architecture/self-hosted-ci.md#차단-게이트)를 따릅니다.

`go work sync` drift는 임시로 복사한 `go.work`·`go.mod`·체크섬의 변화를 검사합니다. 오프라인(`GOPROXY=off`)에서는 직접 VCS 조회를 하지 않습니다. Go는 동기화 중 각 모듈을 개별적으로 다시 열기 때문에 workspace의 `replace`만으로는 캐시에 없는 버전의 매니페스트를 찾지 못합니다. 이 경우에만 API의 Valkey 어댑터 핀과 workspace의 `use`·버전별 `replace`가 일치하는지, 형제 어댑터 `go.mod`의 Go 체크섬이 API `go.sum`의 단일 핀과 일치하는지 확인해 API 임시 복사본에 같은 버전의 `replace`를 둡니다. 온라인은 설정된 프록시 정책을 그대로 따릅니다. 이 검사는 모듈 ZIP이나 게시 상태를 검증하지 않습니다. `GOWORK=off` tidy·vet·운영 빌드는 별도이며 정식 의존성이 필요합니다.

`pre-push-gate.sh`는 변경 입력에 맞춰 릴리스 판본·배포 스크립트를 검사하고 제품 변경에는 `govulncheck`도 실행합니다. 정확히 문서·버전만 바뀌면 Python 준비와 제품 검사를 생략하되 버전 변경은 모듈별 빌드로 확인합니다. `RUN_INTEGRATION_TESTS=true`이면 일회용 PostgreSQL·Valkey를 생성해 integration 테스트를 실행하고 회수합니다. `TEST_DATABASE_URL`을 지정한 경우 해당 DB의 소유권 검증을 거쳐 dispatch outbox integration 테스트를 실행합니다.

## 2. architecture/
PR/릴리스 전 제품 결함 게이트와 운영 검증 기록 렌더링 도구입니다. 공개 GitHub Release 본문은 [릴리즈 runbook](../docs/current/runbooks/release.md)의 GitHub 자동 생성 절차를 따릅니다. `render-release-notes.sh`는 별도 운영 기록용입니다.

- 표준 진입점: `./scripts/architecture/ci-boundary-gate.sh`
- 검사별 유지 근거: [CI gates](../docs/current/architecture/ci-gates.md)

## 3. deploy/
중앙 Quadlet 설치와 AP 배포 스크립트입니다.

- `sudo ./scripts/deploy/sync-opt-current.sh` - 중앙 배포 트리·Quadlet unit·prestart 설치(`deploy/quadlet/README.md`)
- `scripts/deploy/quadlet-prestart.sh` - 중앙 unit의 ExecStartPre/ExecCondition helper
- `sudo -n /opt/hololive-bot/compose/app-sources/<승인 SHA>/scripts/deploy/quadlet-redeploy-service.sh <hololive-api|hololive-alarm-worker> <image-id> [<api-image-id>]` - 봉인된 소스와 같은 후보의 읽기 전용 스키마 검사 후 해당 writer만 교체합니다. 이전 pair를 보존하며 자동 복구하지 않습니다.
- 같은 세대의 `quadlet-rollback-service.sh <release> --check`로 캡처한 pair를 검사합니다. `--apply`에는 별도 승인이 필요합니다. 보호 세대 게시 전 일반 설치의 명령은 [릴리스 런북](../docs/current/runbooks/release.md)을 참조합니다.
- 원격 AP(`youtube-collector` fleet) 호스트 운영: `scripts/deploy/ap-hosts/<host>.conf` 기반
  - Osaka native: `./scripts/deploy/ap-host-native-deploy.sh osaka [--dry-run|--apply]`
  - `./scripts/deploy/ap-completion-check.sh <host>`
  - Osaka native rollback: `./scripts/deploy/ap-host-native-rollback.sh osaka [--dry-run|--apply]`
- Quadlet collector b(iris-seoul)·c(중앙): `PO_<B|C>_SSH_TARGET=<host> APPROVE_PO_<B|C>_DEPLOY=true ./scripts/deploy/po-central-cutover.sh <b|c> deploy`, `check|rollback <backup>`

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
- `./scripts/logs/ap-logs.sh <host> [youtube-collector|all]`

## 5. review/
리뷰 전달용 source/full bundle export와 사후 검증 스크립트입니다.

- `./scripts/review/export-source-bundle.sh [output_dir]`
- `./scripts/review/export-full-bundle.sh [output_dir]`
- `INCLUDE_UNTRACKED=true ./scripts/review/export-full-bundle.sh [output_dir]`
- `./scripts/review/verify-full-bundle.sh <bundle.tar.gz>`

full exporter는 현재도 `mode: full`과 합산 `content_sha256` manifest를 만듭니다.
verifier의 `legacy` 명칭 분기는 이 현행 생산 형식이며 제거하지 않습니다.
source exporter의 `hololive-review-bundle-v1`도 같은 verifier로 검증합니다.

## 6. runtime/
운영 중 런타임 상태 조회와 안전한 보정 작업용 스크립트입니다.

- `./scripts/runtime/alarm-dispatch-outbox-status.sh`
- `./scripts/runtime/alarm-dispatch-outbox-requeue.sh`
- `./scripts/runtime/alarm-dispatch-outbox-retention.sh`
- `./scripts/runtime/set-iris-base-url.sh`

## 7. maintenance/
수동 적용용 PostgreSQL 유지보수 SQL과 정리 도구입니다.

- `hololive_msa_hot_path_observability.sql`
- `prune-ap-artifacts.sh` - `ap-host-native-deploy.sh`가 남긴 빌드 페이로드를 최신 N개만 남기는 수동 정리(기본 dry-run)

## 8. refactor/
인증 값·응답 원문의 로그 유출을 검사합니다.

- `./scripts/refactor/grep-sensitive-logs.sh`

## 9. systemd/
호스트 배포용 systemd unit 정본입니다.

- 중앙 container unit은 `deploy/quadlet/hololive-bot/`(Quadlet)이 소유합니다. `admin-dashboard-ingress-firewall.service`는 여기 있습니다.
- `hololive-daily-log-rollup.service`/`.timer`
- `hololive-main-log-mirror@.service`/`.timer`
- `admin-dashboard-ingress`는 중앙 Quadlet의 host-networked Nginx 컨테이너로 중앙 단축 링크 `100.100.1.8:30192`만 제공합니다. 관리 앱은 iris-seoul의 Iris Console(`iris-console.service`)이 소유합니다.

정리 원칙:
- retired/no-op 스크립트는 유지하지 않습니다.
- 운영 표준 진입점은 README와 runbook에 문서화된 것만 남깁니다.
