# Scripts

루트 `scripts/`는 현재 11개 영역을 운영합니다: `ci/`, `architecture/`, `deploy/`, `logs/`, `review/`, `runtime/`, `smoke/`, `maintenance/`, `ops/`, `refactor/`, `systemd/`.

GitHub Release 게시·본문 수정: `bash scripts/publish-release.sh <tag> <previous-tag> [preview|create|edit]`. 기본은 미리보기이며 [릴리즈 runbook](../docs/current/runbooks/release.md)의 승인·검증 절차를 따릅니다.

## 1. ci/
로컬 CI gate 진입점입니다. `build-all.sh`는 image build(`podman build`) 전에 이 gate를 실행합니다.

- `./scripts/ci/local-ci.sh`

기본 gate는 architecture gates, manifest가 선택한 Go toolchain, `go work sync` drift, `go mod tidy -diff`, 모듈별 `GOWORK=off go vet`, stage-3 `golangci-lint`(staticcheck·formatter·modernize 포함), NilAway, `go build`, PGO-off production policy와 race 테스트를 포함합니다. `GOTOOLCHAIN` 기본값은 `auto`이며 명시한 환경값을 유지하고 `go.mod`·`go.work`를 스크립트의 고정 버전으로 수정하지 않습니다. `go-test-nonrace.sh`는 Go가 선택한 테스트 파일을 비교해 race 빌드에서 빠지는 테스트가 있는 패키지만 일반 테스트로 보완합니다. NilAway는 `go vet -vettool`의 unitchecker로 의존성 분석을 재사용하며 기본 분석 병렬도는 1입니다.

`pre-push-gate.sh`가 배포·운영 스크립트 테스트와 `govulncheck`를 추가로 실행합니다. `RUN_INTEGRATION_TESTS=true`이면 일회용 PostgreSQL·Valkey를 생성해 integration 테스트를 실행하고 회수합니다. `TEST_DATABASE_URL`을 지정한 경우 해당 DB의 소유권 검증을 거쳐 dispatch outbox integration 테스트를 실행합니다.

## 2. architecture/
PR/릴리스 전 제품 결함 게이트와 운영 검증 기록 렌더링 도구입니다. 공개 GitHub Release 본문은 [릴리즈 runbook](../docs/current/runbooks/release.md)의 GitHub 자동 생성 절차를 따릅니다. `render-release-notes.sh`는 별도 운영 기록용입니다.

- 표준 진입점: `./scripts/architecture/ci-boundary-gate.sh`
- 검사별 유지 근거: [CI gates](../docs/current/architecture/ci-gates.md)

## 3. deploy/
중앙 Quadlet 설치와 AP 배포 스크립트입니다.

- `sudo ./scripts/deploy/sync-opt-current.sh` - 중앙 배포 트리·Quadlet unit·prestart 설치(`deploy/quadlet/README.md`)
- `scripts/deploy/quadlet-prestart.sh` - 중앙 unit의 ExecStartPre/ExecCondition helper
- `sudo /opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh <hololive-api|hololive-alarm-worker> <image> [<api-image>]` - 중앙 API·worker image 교체(이전 pair 보존, 활성화 전 migration, 실패 시 이전 pair 복원)
- `sudo /opt/hololive-bot/compose/current/scripts/deploy/quadlet-rollback-service.sh <release> [--check|--apply]` - 보존한 이전 pair로 API·worker rollback
- 원격 AP(`youtube-collector` fleet) 호스트 운영: `scripts/deploy/ap-hosts/<host>.conf` 기반
  - Osaka native: `./scripts/deploy/ap-host-native-deploy.sh osaka [--dry-run|--apply]`
  - `./scripts/deploy/ap-completion-check.sh <host>`
  - Osaka native rollback: `./scripts/deploy/ap-host-native-rollback.sh osaka [--dry-run|--apply]`
  - `./scripts/deploy/ap-iris-h3-trust-preflight.sh <host>`
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
수동 적용용 PostgreSQL 유지보수 SQL입니다.

- `hololive_msa_hot_path_observability.sql`

## 8. ops/
PostgreSQL failover 운영 자산입니다.

- `./scripts/ops/postgres-failover.sh` (+ non-root launcher, `.service`/`.timer`, apply/env examples, `postgres-failover_test.sh`)
- `./scripts/ops/postgres-failover-fence-ssh.sh` - reachable primary용 reference fence hook
- `./scripts/ops/postgres-primary-fence.sh` - 구 primary에서 Quadlet unit/DB 재기동을 영속 차단하는 remote action
- `./scripts/ops/postgres-primary-unfence.sh` - 재시딩·streaming 검증 뒤 fence generation을 안전하게 해제하는 root helper
- `./scripts/maintenance/postgres-failover-db-role.sql` - controller DB role에 CONNECT와 `pg_promote()`만 부여하는 수동 bootstrap

PostgreSQL controller는 Docker 권한 없는 `hololive-pg-failover` 사용자로 실행되고 checked-in
unit은 dry-run입니다. 실제 승격은 승인된 external fencing과
route hook을 모두 준비한 뒤 apply drop-in을 설치해야 합니다. 운영 계약은
`docs/current/runbooks/postgres-replication.md`가 소유합니다.

## 9. refactor/
인증 값·응답 원문의 로그 유출을 검사합니다.

- `./scripts/refactor/grep-sensitive-logs.sh`

## 10. systemd/
호스트 배포용 systemd unit 정본입니다.

- 중앙 container unit은 `deploy/quadlet/hololive-bot/`(Quadlet)이 소유합니다. `admin-dashboard-ingress-firewall.service`는 여기 있습니다.
- `hololive-daily-log-rollup.service`/`.timer`, `hololive-osaka-daily-log-rollup.service`/`.timer`
- `hololive-main-log-mirror@.service`/`.timer`
- `admin-dashboard-ingress`는 중앙 Quadlet의 host-networked Nginx 컨테이너로 중앙 단축 링크 `100.100.1.8:30192`만 제공합니다. 관리 앱은 iris-seoul의 Iris Console(`iris-console.service`)이 소유합니다.

정리 원칙:
- retired/no-op 스크립트는 유지하지 않습니다.
- 운영 표준 진입점은 README와 runbook에 문서화된 것만 남깁니다.
