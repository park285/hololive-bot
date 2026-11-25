# 게이트 셸 테스트의 Go 이관과 압축 계획

작성 2026-11-24, 기준 main `784def389`. 실행은 아래 선행 조건이 풀린 뒤 시작합니다.

## 목표

pre-push 게이트가 실행하는 배포·로그·빌드 셸 테스트를 Go 테스트로 옮깁니다. 같은 동작을 검사하는 중복 케이스는 합치고, 게이트 실행 시간을 줄입니다.

검사하는 동작은 줄이지 않습니다. `docs/current/architecture/ci-gates.md`의 "배포·로그·failover 시험은 실행 결과와 부작용을 검사하므로 유지" 결정을 지킵니다. 테스트를 삭제하는 것이 아니라 언어와 실행 구조를 바꾸는 작업입니다.

완료 기준:

- 아래 표의 이관 대상 셸 테스트 25개가 없고, `pre-push-gate.sh`의 `run_if_changed`에는 Python 테스트·release version만 남습니다.
- `scripts/architecture/ci-boundary-gate.sh`에는 검사 3개(추적 산출물, SQL 소유권, topology parity)만 남습니다.
- 기존 케이스 라벨과 Go subtest의 대응표에 누락이 없습니다. 합친 케이스는 합친 이유를 함께 적습니다.
- 스크립트 테스트 구간 시간이 이관 전보다 짧습니다. 같은 부하 조건에서 3회 측정한 중앙값으로 비교하고, 구체 목표값은 Phase 0 측정 뒤 이 문서에 적습니다. 시간 단축 효과는 아직 입증되지 않았습니다(아래 "효율화의 근거와 한계").
- GitHub Languages와 같은 바이트 계산으로 Shell 비중이 약 8.6%에서 약 5.5%로 내려갑니다.

## 현재 상태와 근거

- 추적 중인 Shell 1.17MB 가운데 게이트가 실행하는 테스트가 27개, 432KB이고 게이트 스크립트(`scripts/ci`, `scripts/architecture`)가 114KB입니다.
- 저장소 규칙이 금지하는 게이트(소스 문자열 단언, 검사기 자체 시험, 제거 이름 검사)는 찾지 못했습니다. 27개 모두 fixture에서 스크립트를 실행하고 결과와 부작용을 검사합니다.
- `collector-cutover-failure_test.sh`(118KB)는 거의 전부가 Python heredoc이며 Shell로 집계됩니다.
- 시간 근거는 `perf/gate-speedup` 세션의 full pre-push 실측입니다. 측정 중 load average가 4~27이어서 잡음이 있습니다.

| 테스트 | 시간 |
|---|---|
| `ap-host-native-deploy_test.sh` (아래 3개를 다시 실행함) | 450초 |
| `quadlet-redeploy-service_test.sh` | 178초 |
| `admin-bind_test.sh` | 70초 |
| `sync-opt-current_test.sh` | 31초 |
| `pg-major-upgrade_test.sh` | 7초 |
| 나머지 셸 테스트 합계 | 약 7초 |

`collector-cutover-failure_test.sh`는 단독 실행에 약 55초가 걸립니다. 같은 측정에서 Go 단계(golangci-lint, NilAway, build, vet)는 각각 10초 미만이었습니다. 다만 이 측정에는 Go race 시험과 govulncheck 시간이 없으므로, 셸 테스트가 게이트 전체 시간의 대부분이라고 단정할 수는 없습니다.

구조 문제는 네 가지입니다.

1. 셸 테스트를 직렬로 실행합니다.
2. `ap-host-native-deploy_test.sh`가 다른 테스트 파일 3개를 중첩 실행합니다.
3. `test-compose-services.sh`를 pre-push와 `ci-boundary-gate.sh`가 두 번 실행합니다.
4. 테스트마다 `systemctl`·`podman`·`ssh` 가짜 명령을 따로 구현합니다.

첫째와 셋째 문제는 `perf/gate-speedup`이 처리하고 있습니다.

## 효율화의 근거와 한계

Go로 옮겨도 테스트가 실행하는 bash 자체는 빨라지지 않습니다. `perf/gate-speedup`의 측정에서 `collector-cutover-failure_test.sh`를 두 개 동시에 실행하자 101.6초가 걸렸습니다. 하나만 실행할 때는 약 54초였으므로, 이 harness는 CPU를 다 쓰고 있어 병렬화만으로는 빨라지지 않습니다.

시간을 줄일 수 있는 후보는 다음과 같습니다.

- 중첩 실행과 중복 실행을 없앱니다.
- 같은 동작을 검사하는 케이스를 합칩니다.
- 케이스마다 반복되는 프로세스 생성을 줄입니다. 예를 들면 Python 인터프리터 기동과 함수 추출입니다.
- 대기 루프를 타는 케이스를 고칩니다.

Phase 0은 진행 여부를 판단하는 단계입니다. 느린 묶음마다 CPU 시간(user+sys)과 벽시계 시간을 비교해 시간이 계산에 쓰이는지 대기에 쓰이는지 가립니다. 줄일 여지가 없으면 시간 목표는 버리고, Shell 비중과 중복 정리만 목표로 남깁니다.

`go test`는 패키지마다 기본 10분 시간 제한을 둡니다. AP 묶음은 지금 450초가 걸리므로 패키지를 나누거나 `-timeout`을 명시합니다.

## 설계 결정

1. **새 Go 모듈 `scripts/scripttest`** (`github.com/kapu/hololive-scripttest`)를 만들고 표준 라이브러리만 씁니다. 스크립트는 어느 서비스 모듈에도 속하지 않습니다. 표준 라이브러리만 쓰면 새 의존성 승인이 필요 없습니다. `go.work`에 넣으면 `GO_OWNED_MODULES`를 통해 tidy, vet, golangci-lint, NilAway, govulncheck가 자동으로 적용됩니다.
2. **공용 harness `internal/harness`**는 다음 기능을 제공합니다.
   - `t.TempDir` fixture
   - 호출을 기록하는 가짜 명령 생성
   - 환경 변수·PATH·stdin을 지정한 스크립트 실행
   - lib를 source한 뒤 함수 호출
   - 호출 순서 단언

   테스트는 실행 결과와 부작용만 단언하고, 스크립트 본문 문자열은 단언하지 않습니다.
3. **호스트 의존 테스트**는 build tag `hosttest`로 분리합니다. 대상은 systemd user bus, `systemd-run`, rootless podman 이미지 빌드가 필요한 테스트입니다. pre-push는 kapu에서 `-tags hosttest`로 실행하고, GitHub CI는 태그 없이 실행합니다. 태그 파일도 vet 대상에 넣습니다. `t.Skip`으로 숨기지 않습니다.
4. **변경 범위 선택**은 `scripts/ci/local-ci-packages.sh`의 `configure_go_packages`가 맡습니다. 이 함수에 경로→scripttest 패키지 매핑을 추가하고, 각 패키지의 입력에는 지금의 `run_if_changed` 입력 목록을 그대로 옮깁니다. 이렇게 하면 AP 묶음은 지금처럼 AP 관련 입력이 바뀔 때만 실행됩니다.
5. **GitHub CI에는 기본적으로 넣지 않습니다.** 지금처럼 로컬 pre-push에서 변경 범위에 따라서만 실행합니다. `ci.yml`의 `go-modules` matrix는 모든 PR에서 모듈 전체를 범위 구분 없이 실행하므로, 여기에 넣으면 게이트가 오히려 늘어납니다. matrix와 `public-pr-go-gate.sh` 허용 목록에 추가할지는 사용자가 정합니다(미결). 모듈 tidy·vet·lint는 local-ci의 `GO_OWNED_MODULES` 경로로 검사합니다.
6. **케이스는 `t.Run`과 `t.Parallel`로 실행합니다.** 일부 케이스는 스크립트의 대기 루프에 걸릴 수 있습니다. 예를 들어 `quadlet-redeploy-service.sh`의 `started_generation`은 0.25초 간격으로 polling하고, `quadlet-prestart.sh`의 tailnet 대기는 1초씩 90회 반복합니다. 이런 케이스는 Phase 0 프로파일로 찾아내고, 가짜 명령이 기대 상태를 바로 돌려주게 만듭니다. 테스트를 위해 스크립트에 설정을 추가하지는 않습니다.

## 이관 대상과 순서

| 패키지 | 대상 셸 테스트 | Phase | 경합 브랜치 |
|---|---|---|---|
| `deploy/env` | `test-compose-env.sh`, `test-compose-services.sh`, `lib/require-quic-udp-buffer_test.sh`, `lib/public-bind-mounts_test.sh`, `lib/youtubejs-node-version_test.sh`, `source-revision-provenance_test.sh`, `hololive-api/scripts/test-bot-env-loader.sh` | 1 | `perf/gate-speedup`(`test-compose-services` 실행 위치) |
| `logs` | `daily-rollup-logs_test.sh`, `test-stream-mirror-retention.sh`, `test-remote-sync-main-logs.sh` | 1 | `perf/gate-speedup` |
| `build` | `build-all_test.sh`, `build-hololive-collector-go_test.sh`, `image-runtime-tree-permissions_test.sh`(hosttest) | 1 | 대상 `build-all.sh`만 여러 브랜치가 수정 |
| `runtime` | `set-iris-base-url_test.sh` | 1 | 없음 |
| `deploy/prestart` | `admin-bind_test.sh`, `pg-major-upgrade_test.sh` | 2 | 경합 브랜치 전부 |
| `deploy/syncopt` | `sync-opt-current_test.sh`, `verify-exec-tree-ownership_test.sh` | 2 | 경합 브랜치 전부 |
| `deploy/quadlet` | `quadlet-redeploy-service_test.sh` | 2 | 경합 브랜치 전부 |
| `deploy/apnative` | `ap-host-native-deploy_test.sh`, `ap-host-native-cutover_test.sh`, `collector-cutover-failure_test.sh`, `ap-host-native-collector-wrapper_test.sh`, `ap-completion-check_test.sh`, `lib/ap-host_test.sh` | 3 | 경합 브랜치 전부와 `perf/gate-speedup` |

"경합 브랜치"는 `perf/db-stage0`(main보다 92커밋 앞섬), `perf/audit-fadeout`(89커밋), `preserve/pgbouncer-retirement-steps-1-9`(3커밋)입니다. 2026-11-24에 `git diff --name-only main...<branch>`로 확인했습니다. 세 브랜치 모두 Phase 2·3 테스트 파일과 그 대상 스크립트(`ap-host-native-remote-apply.sh`, `po-central-remote.sh`, `quadlet-prestart.sh`, `quadlet-redeploy-service.sh`, `sync-opt-current.sh` 등)를 수정합니다. 앞의 두 브랜치는 `go.work`도 수정합니다.

`release/hololive-pool-only-030`도 같은 파일을 수정했지만, main과 두 점 diff로 비교하면 `quadlet-redeploy-service_test.sh`의 차이가 191줄만 남습니다. 대부분 이미 main에 반영된 것으로 보고 경합에서 제외합니다.

이관하지 않는 테스트:

- `deploy/images/postgres/test.sh`, `scripts/ci/test-infra-images.sh`: 인프라 image를 공식 image로 바꾸면서 지웠습니다.

### Phase 0: 준비

- [ ] `perf/gate-speedup`이 main에 합쳐진 것을 확인합니다. 이 브랜치는 pre-push 병렬 실행, 중복 실행 제거, `collector-cutover-failure_test.sh` 수정을 커밋하지 않은 상태로 진행 중입니다.
- [ ] Phase 2·3은 `perf/db-stage0`과 `perf/audit-fadeout`이 합쳐지거나 폐기된 뒤 시작합니다. 시작할 때 경합 브랜치 목록을 다시 확인합니다.
- [ ] `tools/ops/stack-worktree.sh add <branch> hololive-bot`로 작업 worktree를 만들고, 이 계획 파일을 그 브랜치로 옮깁니다.
- [ ] 이관 전 기준선을 기록합니다. 27개 테스트 각각의 시간(3회)과 케이스 라벨 목록(`[PASS]`·`require`·`fail` 메시지)을 남깁니다.
- [ ] 느린 세 묶음(AP native, quadlet-redeploy, admin-bind)에서 어느 케이스와 대기가 시간을 쓰는지 분해합니다. CPU 시간과 벽시계 시간도 비교하고, 진행 여부와 목표 시간을 이 문서에 적습니다.
- [ ] race 시험과 govulncheck를 포함한 full pre-push 전체 시간을 측정해, 셸 테스트가 차지하는 비율을 확정합니다.

### Phase 1: 모듈, harness, 배선, 소형 테스트

- [ ] 모듈, `go.work`·`go.work.sum`, `configure_go_packages` 매핑, pre-push의 `go test` 실행(`-timeout` 명시), `hosttest` 실행·vet 경로를 추가합니다.
- [ ] 표의 Phase 1 테스트를 이관합니다. 각 이관 커밋에서 옛 셸 파일과 `run_if_changed`·`ci-boundary-gate.sh` 항목을 함께 지웁니다.

### Phase 2: 중형 테스트

경합 브랜치가 정리된 뒤 진행합니다.

- [ ] `admin-bind`와 `pg-major-upgrade`를 이관합니다.
- [ ] `sync-opt-current`와 `verify-exec-tree-ownership`을 이관합니다.
- [ ] `quadlet-redeploy-service`를 이관합니다.

### Phase 3: AP native 묶음 병합

- [ ] 세 파일은 `ap-host-native-remote-apply.sh`와 `po-central-remote.sh`의 함수를 정규식이나 awk로 잘라 실행합니다. 이 추출을 harness 한 곳으로 모으고, 같은 함수와 같은 시나리오를 검사하는 케이스는 하나로 합칩니다.
- [ ] 원격 적용 스크립트를 source할 수 있게 바꿀 수 있는지 확인합니다. 이 스크립트들은 `ssh`의 `bash -s` 본문으로 전송될 수 있어서 구조가 충돌할 수 있습니다. 바꿀 수 없으면 함수 추출 방식을 유지합니다.
- [ ] 중첩 실행을 없앱니다.

### Phase 4: 정리

- [ ] `ci-gates.md`, README의 검증 절, runbook과 skill에 남은 옛 테스트 경로 참조를 갱신합니다.
- [ ] 마지막에 `git grep`으로 옛 경로 참조가 하나도 남지 않았는지 확인합니다.

## 검증

- 이관마다 새 Go 테스트가 통과하는지 확인합니다. 대상 스크립트에 일회성 결함을 넣어 옛 셸 테스트와 새 Go 테스트가 모두 실패하는지도 확인하고, 결함은 되돌립니다. 결함 주입은 커밋하지 않고 게이트에도 넣지 않습니다.
- 케이스 대응표를 PR 본문에 싣습니다.
- 모듈 검사로 golangci-lint, NilAway, `go vet`(태그 포함), `go test -race -count=1`, `scripts/ci/shell-syntax-sweep.sh`, `bash scripts/ci/verify-tools.sh scan`을 실행합니다.
- 마지막으로 `FULL_PRE_PUSH=true bash scripts/ci/pre-push-gate.sh`의 이관 전후 시간을 비교하고, Shell 비중을 다시 계산합니다.
- Go 코드에는 `use-modern-go` 스킬을 적용합니다. 주석은 쓰지 않습니다.

## 범위 밖

- Python 테스트 2개(`worker-profile-pair_test.py`, `po-sandbox-manifest_test.py`): 대상이 Python 스크립트입니다.
- 게이트 밖 수동 테스트 4개:
  - `test-pgbouncer.sh`: PgBouncer 퇴역 브랜치가 삭제합니다.
  - `test-postgres-access-policy.sh`, `test-terminal-live-head-repair.sh`: 직접 실행하는 수동 테스트로 남깁니다.
  - `systemd-stop-guard_test.sh`: 어느 게이트도 실행하지 않고 sudo가 필요합니다.
- 배포 스크립트 자체의 Go 이식: Shell 비중을 약 2.7%까지 낮출 수 있지만 따로 결정할 일입니다.
- `scripts/ci`·`scripts/architecture` 검사 스크립트: 금지 항목이 없으므로 유지합니다.

## 미결

- `preserve/pgbouncer-retirement-steps-1-9`를 다시 살릴지 정해야 합니다. 살린다면 이 브랜치를 먼저 합친 뒤 Phase 2·3을 진행합니다. 버린다면 경합 목록에서 뺍니다.
- 이관한 테스트를 GitHub PR CI(`ci.yml` matrix)에서도 실행할지 정해야 합니다. 실행하면 검증 범위가 넓어지는 대신, 모든 PR에서 범위 구분 없이 돌아 CI 시간이 늘어납니다.

## 승인

- worktree 생성, 로컬 편집, 빌드, 테스트는 승인 없이 진행합니다.
- 커밋, push, PR 생성, main 통합은 사용자가 요청할 때만 합니다.
