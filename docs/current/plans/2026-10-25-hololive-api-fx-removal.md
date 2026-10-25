# hololive-api Fx 제거와 공유 bootstrap 통일

**상태: 구현·검증 완료, PR 게시(2026-10-25).** 브랜치 `refactor/hololive-api-fx-removal`. logger 닫기 오류 수정은 shared-go v2.16.1(`1c1b78c`)로 릴리스해 이 PR에서 pin을 갱신했습니다. 배포는 포함하지 않습니다. 결과는 맨 아래 [진행 결과](#진행-결과)에 있습니다.

## 결정

hololive-api의 `go.uber.org/fx`를 제거하고, collector·alarm-worker와 같은 `shared-go` `bootstrap.Options` + `lifecycle.Run` + `hololive-shared` `observability.ManagedRuntime` 조립으로 통일합니다. 반대 선택지였던 "세 서비스 모두 Fx 도입"은 채택하지 않습니다.

## 근거

조사 기준은 hololive-bot `main` `dd5669144`입니다.

- **사용처가 한 곳이고 DI로 쓰이지 않습니다.** Fx는 `hololive/hololive-api/internal/fxapp/`(비테스트 6파일 약 800줄, 테스트 약 1,200줄)에만 있고 진입점은 `cmd/hololive-api/main.go`의 `fxapp.New` 하나입니다. provider는 context·telemetry·runtime·supervisor·lifecycleCoordinator 다섯 개가 일직선이고, 실제 plane 조립(`internal/app/runtime.go`의 `BuildRuntime` → `buildAPIPlanes`)과 자원 회수(`resourceOwner`)는 Fx 밖의 손으로 쓴 Go입니다. Fx가 맡는 것은 signal 대기, Start/Stop timeout, `Shutdowner`로 exit code 전달뿐입니다.
- **다른 두 서비스는 같은 일을 라이브러리 없이 합니다.** collector `cmd/runtime/youtube-collector/main.go`와 alarm-worker `cmd/alarm-worker/main.go`는 `bootstrap.Options`로 기동하고, runtime의 `Run()`이 `lifecycle.Run`으로 signal·fatal·shutdown을 처리합니다. hololive-api만 다른 길입니다.
- **설계 문서가 이미 범위를 묶어 두었습니다.** [hololive-api 리팩터링 설계](../../design/2026-10-02-hololive-api-refactoring.md) 401행은 "기존 lifecycle/resource-owner 방식을 재사용하고 새 범용 DI/container를 만들지 않는다", 482행은 "fxapp는 프로세스 lifecycle만 유지"입니다. 전면 도입은 이 결정과 어긋나고, 세 서비스에 런타임 의존(fx, dig와 전이 의존 zap·multierr·goleak) 추가 승인이 필요합니다.
- **제거 비용이 작습니다.** `lifecycle.Run`이 fxapp이 보존하던 계약을 이미 제공합니다(아래 대응표). 기존 `fxapp` 테스트가 종료 순서·fatal exit·rollback을 고정하므로 회귀 검출 수단이 있습니다.

## 계약 대응표

Fx가 제공하던 동작과 공유 기반의 대응을 소스에서 대조했습니다.

| fxapp 동작 | 현재 위치 | 공유 기반 대응 | 확인 |
|---|---|---|---|
| runtime errCh의 오류 → exit 1 | `supervisor.handleTerminal` → `fx.ExitCode(1)` | `lifecycle.Run`이 errCh 오류를 반환값으로 보존 → `bootstrap.Options.Run`이 1 반환 | `skeleton.go` `waitForStop`, `bootstrap.go` `Run` |
| errCh의 nil → 정상 종료 exit 0 | `handleTerminal(nil)` → `requestShutdown()` | `waitForStop`이 nil을 그대로 반환 → exit 0 | 같은 의미 |
| SIGINT/SIGTERM 대기 | `fx.App.Wait` | `lifecycle.Run` 기본 signal 구독 | `signalSubscription` |
| 종료 순서: runtime cancel → `runtime.Shutdown(drain 30s)` → supervisor stop → resources close | `lifecycleCoordinator.OnStop` | stop 신호 → runtime ctx cancel → `Shutdown` hook(ShutdownTimeout) → `bootstrap`의 `defer rt.Close()` → `ManagedRuntime.Close`(runtime Close 후 telemetry shutdown) | `doc.go` 종료 순서 계약 |
| Stop timeout 30s, 그 안에서 plane drain 10s | `fx.StopTimeout(processLifecycleTimeout)` + `drainLimit = constants.AppTimeout.Shutdown` | `lifecycle.Options.ShutdownTimeout = processStopTimeout`(30s), `Shutdown` hook 안에서 drain만 `constants.AppTimeout.Shutdown`(10s)로 제한 | 구현 중 정정: `AppTimeout.Shutdown`은 10s라 그대로 쓰면 전체 예산이 30s에서 10s로 줄어듦 |
| Start timeout 30s | `fx.StartTimeout` | 없음. `Runtime.Start`는 goroutine만 띄우고 반환하므로 timeout이 작동할 지점이 없음 | 의도적 생략 |
| 생성 실패 시 telemetry·plane rollback | `rollbackApplicationConstruction` + `resourceOwner` | `ManagedRuntime.buildRuntime`이 build 실패 시 provider shutdown, `buildAPIPlanes`가 plane rollback | 기존 코드 유지 |
| 미완료 close 재개(resumable) | `resourceOwner.AddResumable(runtime.CloseContext)` | `Runtime.CloseContext`가 `closeInit`/`closeDone`으로 자체 멱등·재합류 | `runtime.go` 216행 |
| Fx 이벤트 로그 | `fxevent.SlogLogger` | 삭제. 공유 bootstrap의 기동·오류 로그만 유지 | iris-stack 안의 알림·설정 의존 없음(아래 진행 결과) |

## 작업

구현은 hololive-bot 소유 worktree에서 합니다. Go 변경은 `modern-go-guidelines:use-modern-go`를 적용합니다.

1. `internal/app.Runtime`에 `Run() error`를 추가합니다. collector `collectorruntime.Runtime.Run`과 같은 꼴로 `lifecycle.Run`에 `Start: r.Start`, `Shutdown: Shutdown 뒤 CloseContext`, `OnSignal` 로그를 넘기고, 종료 원인은 반환값으로만 전달합니다. 기존 `Start`·`Shutdown`·`Close`·`CloseContext` 시그니처는 유지합니다.
2. `cmd/hololive-api/main.go`를 `bootstrap.Options[*apiconfig.RuntimeConfig, *observability.ManagedRuntime[*runtimeapp.Runtime]]`로 바꿉니다. `NewLogger`는 기존 `newHololiveAPILogger`를, telemetry 설정은 `fxapp.hololiveAPITelemetryConfig`를 옮겨 씁니다. 기동 메시지와 필드(`bot_port`·`admin_port`·`llm_port`)는 `StartupMessage`·`StartupFields`로 보존합니다.
3. `internal/fxapp/`를 삭제합니다. `supervisor`·`lifecycleCoordinator`·`resourceOwner`는 `lifecycle.Run`과 `Runtime.CloseContext`가 대신하므로 옮기지 않습니다. 테스트 중 보존할 시나리오는 4단계에서 `internal/app` 테스트로 다시 씁니다.
4. 회귀 테스트를 `internal/app`에 둡니다: errCh 오류 → `Run`이 오류 반환, errCh nil → nil 반환, `Shutdown` 실패와 runtime 오류가 모두 반환값에서 `errors.Is`로 식별, `CloseContext` 두 번 호출의 멱등성. 기존 `fxapp` 테스트 중 Fx 자체 동작(`fx.Shutdowner` 호출 여부 등)을 보는 것은 옮기지 않습니다.
5. `go.mod`에서 `go.uber.org/fx`를 제거하고 `go mod tidy`로 `dig`·`zap`·`multierr`·`goleak` 전이 의존이 사라지는지 확인합니다. 다른 직접 의존이 zap을 끌어오면 그대로 둡니다.
6. 문서: [설계 문서](../../design/2026-10-02-hololive-api-refactoring.md)의 fxapp 구조도(301·312행)와 책임 위치(482행)를 "shared bootstrap/lifecycle"로 고치고, 411행의 완료 기준 "production signal owner가 Fx 하나인 현재 계약 유지"는 이 계획이 대체한다고 적습니다. signal owner가 하나라는 불변식 자체는 유지합니다: production 경로의 `lifecycle.Run` 호출은 aggregate `Runtime.Run` 하나여야 하고, 407행이 언급한 `LLMSchedulerRuntime.Run`의 별도 `lifecycle.Run`은 production entrypoint가 아니어야 합니다. PROJECT_MAP에는 Fx 언급이 없습니다. 구현 중 정정: hololive-api runbook §4, `docs/current/services/hololive-api.md`, `CODEBASE_OVERVIEW.md`에 Fx 언급이 있어 함께 고쳤습니다.

## 검증

- `hololive/hololive-api`에서 `go build ./...`, `go test ./cmd/... ./internal/app/...`, 저장소 lint(`golangci-lint run -c ../../.golangci.yml ./...`)와 적용 중인 NilAway·race 게이트.
- 로컬 기동 확인: 정상 SIGTERM에서 exit 0과 drain 로그, runtime fatal 주입(예: plane 서버 포트 충돌)에서 exit 1. 두 경우의 종료 로그가 collector·alarm-worker와 같은 키로 남는지 확인합니다.
- `./build-all.sh --build-only --no-bump`와 publication 전 `scripts/ci/pre-push-gate.sh`.

## 범위 밖

- plane 조립(`buildAPIPlanes`), 자원 소유권, trust domain, 포트·설정 계약은 바꾸지 않습니다.
- 배포는 이 계획에 포함하지 않습니다. 릴리스·cutover는 `hololive-bot-ops` 절차와 별도 승인으로 진행합니다.
- 이 변경은 collector의 Go·JS 경계 작업([통합 계획](2026-10-25-youtube-collection-and-retention-remediation.md))과 독립이며 순서 의존이 없습니다.

## 남은 확인

- Fx 이벤트 로그(`fxevent`)에 기대는 운영 알림이나 대시보드 질의가 있는지 운영 설정에서 확인하지 않았습니다. 6단계에서 runbook과 함께 봅니다.
- `Runtime.Shutdown` 뒤 `CloseContext`를 `lifecycle.Run`의 `Shutdown` hook 하나에서 순서대로 호출할 때 ShutdownTimeout 30초를 두 단계가 공유합니다. 기존에는 drain 30초와 close가 별도 예산이었으므로, 구현 시 plane drain 실측이 상한에 가까우면 `Shutdown` hook 안에서 drain 예산을 나눕니다.

## 진행 결과

### 변경

- `internal/app/runtime.go`
  - `Runtime.Run`을 추가했습니다. `lifecycle.Run`과 `lifecycleOptions()`를 쓰고, `ShutdownTimeout`은 30s입니다.
  - fatal 원인은 `OnError`에서 기존 메시지 `hololive-api runtime error`로 즉시 기록합니다. 그 밖의 로그는 `Received shutdown signal`(`runtime`·`signal`)과 `hololive-api draining runtime planes`입니다.
  - `shutdownAndClose`는 drain과 회수를 goroutine에서 돌리고 30s 시한에서 끊습니다. Fx `StopTimeout`과 같은 강제 상한입니다. 시한에 끊을 때는 그때까지 쌓인 회수 오류를 함께 반환합니다.
  - `drainAndClose`는 drain을 10s로 제한하고 남은 예산으로 `CloseContext`를 부릅니다. drain이 시한 뒤에 끝나면 회수를 새로 시작하지 않습니다.
  - stop hook이 회수를 맡은 뒤 bootstrap이 부르는 `Close`는 다시 합류하지 않습니다.
- `cmd/hololive-api/main.go`: `bootstrap.Options`와 `observability.ManagedRuntime`으로 바꿨습니다. 다음을 보존합니다.
  - 기동 메시지와 필드(`version`·`log_level`·`bot_port`·`admin_port`·`llm_port`)
  - `Failed to assemble hololive-api runtime`
  - `--check-config`, `--check-worker-profile`

  텔레메트리 식별 테스트는 `main_test.go`로 옮겼습니다. bootstrap이 대신하는 기동 흐름 테스트 3개는 삭제했습니다.
- `internal/fxapp/`를 삭제했습니다. `go.mod`에서 `go.uber.org/fx`와 전이 의존 `dig`·`multierr`·`zap`을 제거했습니다. `goleak`은 go.sum에서만 빠졌습니다.
- 테스트 `internal/app/runtime_run_test.go`
  - runtime fatal은 오류를 반환하고, drain 전에 원인 로그를 남깁니다.
  - clean terminal과 SIGTERM은 nil을 반환하고 drain 로그를 남깁니다.
  - fatal·drain·close 오류를 `errors.Is`로 각각 식별할 수 있습니다.
  - 종료 순서는 runtime ctx cancel → drain → close입니다.
  - `Run` 뒤 `Close`는 회수를 반복하지 않습니다.
  - synctest로 `lifecycleOptions()` 경로를 시험합니다: drain 10s와 회수가 남은 예산을 쓰는 분할, ctx를 무시하는 plane에 대한 30s 강제 시한, 시한 뒤 회수를 새로 시작하지 않음, 시한 경로의 회수 오류 보존. 이때 `Close`를 포함한 전체 시간도 30s인지 확인합니다.
  - 각 테스트가 회귀를 잡는지 일회성 변이로 확인했습니다. 변이는 `ShutdownTimeout` 10s, `Close` 재합류, 강제 시한 제거, `OnError` 제거였습니다.
- 문서: 설계 문서(구조도, 근거 링크, 완료 기준, 책임 위치), runbook §4, services 문서의 종료 동작, CODEBASE_OVERVIEW, CHANGELOG `미출시`.
- shared-go 브랜치 `fix/logger-close-error-visibility`(미커밋, `.worktrees/refactor-hololive-api-fx-removal/shared-go`)
  - `bootstrap.Options.Run`이 logger closer 오류를 버리던 것을 고쳐, `Failed to close logger: <redacted>`를 stderr에 남깁니다. exit code는 그대로입니다.
  - `logging`의 `multiCloser.Close`가 처음 실패한 lane 오류만 남기던 것을 `errors.Join`으로 바꿨습니다.
  - 소비자: hololive 3개 서비스, twentyq-bot `cmd/twentyq`·`cmd/twentyq-migrate`, chat-bot-go-kakao(closer 오류 집계).

### 적대적 리뷰

독립 reviewer가 두 차례 검토했습니다.

1차에서 지적된 결함과 처리:
- 종료 hook에 강제 시한이 없었습니다(Fx의 `withTimeout`과 달리 `lifecycle.Run`은 hook을 동기로 기다림). → 강제 시한을 넣었습니다.
- fatal 원인이 즉시 기록되지 않았습니다. → `OnError`에서 즉시 기록합니다.
- `Close`가 다시 합류해 최악 종료가 47s로 Compose grace 45s를 넘었습니다. → 재합류를 없애 최악 약 37s(30 + telemetry 5 + async stdout close 2)입니다.
- 오류가 두 번 기록됐습니다. → 재합류 제거로 해소했습니다.
- 문서·CHANGELOG의 시한 서술이 틀렸습니다. → 고쳤습니다.
- 다음 두 가지는 CHANGELOG·runbook에 적었습니다. 둘 다 shared lifecycle 공통 동작입니다.
  - telemetry flush가 실패해도 exit code는 0입니다.
  - drain 중 두 번째 신호가 오면 즉시 종료합니다.

2차에서 지적된 결함과 처리:
- 시한 경로에서 회수 단계 오류가 사라졌습니다. → 반환값에 함께 싣습니다.
- 버려진 drain이 시한 뒤에 회수를 새로 시작했습니다. → 시작하지 않습니다.
- 두 결함 모두 실패하는 테스트로 먼저 재현한 뒤 고쳤습니다. services 문서와 shared-go CHANGELOG 표현도 고쳤습니다.

남겨 둔 지적:
- bootstrap의 stderr 쓰기는 시한이 없습니다. 기존 `printBootstrapError`와 같은 방식이고, 오류는 lumberjack 파일 닫기에서만 생겨 도달 확률이 낮습니다.
- 성공과 시한 만료가 같은 순간에 준비되면 select가 무작위로 고릅니다. 나노초 단위 창이라 무시할 수준입니다.

### 남은 확인의 결론

- **예산 공유:** 해당하지 않습니다. 기존 Fx도 drain ctx를 stop ctx에서 파생했으므로 drain과 close가 30s 하나를 공유했습니다. 새 구조도 같습니다.
- **Fx 로그 의존:** iris-stack 전체(`Iris/` 제외)에서 `fxevent`·`OnStart hook`·`Fx stop`·`shutdown signal received` 문자열은 과거 기록 문서에만 있습니다. 저장소 밖에 저장된 Grafana/Loki 질의는 보지 않았습니다.
- **signal owner:** production 경로의 `lifecycle.Run`은 `Runtime.Run` 하나입니다. `AdminAPIRuntime.Run`은 production 호출처가 없는 기존 코드라 손대지 않았습니다.
- **기존 부정확함:** services 문서의 "Plane `CloseContext` receives the remaining process-stop context"는 HEAD 때부터 실제와 다릅니다. `CloseContext`는 단계에 `WithoutCancel` ctx를 넘깁니다. 이번 범위 밖입니다.
- **twentyq-bot:** `app.WithLogCloser`는 호출처가 없는 기존 코드입니다.

### 검증(마지막 코드 수정 이후)

- hololive-bot `LOCAL_CI_GO_SCOPE=changed bash scripts/ci/local-ci.sh` 통과(6개 모듈).
  - 포함: go mod tidy diff, canonical vet(GOWORK=off), integration-tag vet, golangci-lint 0건, NilAway, Go build, race·non-race 테스트.
  - 통합 테스트는 기본값 `RUN_INTEGRATION_TESTS=false`라 생략됐습니다.
  - 중간 실행에서 나온 lint 지적(`wsl_v5`, `wrapcheck`)은 모두 테스트 파일이었습니다. 고친 뒤 처음부터 다시 실행했습니다.
- `go test -race -count=3 ./internal/app/ ./cmd/hololive-api/` 통과.
- `./build-all.sh --build-only --no-bump --skip-local-ci hololive-api` 통과.
  - 같은 트리에서 local-ci를 따로 통과했으므로 build-all 안의 local-ci는 생략했습니다.
  - 이미지는 `GOWORK=off`로 고정 shared-go v2.16.0을 씁니다.
- shared-go(`GOWORK=off`):
  - `make lint` 0건, `make test-race`(36개 패키지), `make test-allocations`, `make build` 통과.
  - `make vulncheck`: 영향받는 취약점 0건.
  - 이후 바뀐 것은 CHANGELOG 문구뿐입니다.
- 새 shared-go를 소비자에 연결해 시험했습니다. scratchpad의 임시 go.work를 썼고 저장소 파일은 바꾸지 않았습니다.
  - twentyq-bot: `go build ./...`, `cmd/...`·`internal/twentyq/app/...` 테스트 통과.
  - chat-bot-go-kakao: `go build ./...`, `cmd/...`·`internal/app/...` 테스트 통과.

### 실행하지 않은 검증과 남은 작업

- **실제 프로세스 기동** (SIGTERM → exit 0, 포트 충돌 → exit 1)
  - 실행하지 않은 이유: 로컬 기동은 운영 PostgreSQL·Valkey·Iris에 붙어야 해서 승인 없이는 하지 않았습니다.
  - 대신 확인한 것: `Runtime.Run`과 lifecycle 옵션 단위 테스트, shared-go bootstrap의 exit code 계약.
  - 남은 확인: cutover smoke에서 실제 기동을 봐야 합니다.
- **publication 게이트:** shared-go `scripts/ci/release-gate.sh`는 v2.16.1 tag 전에 통과했습니다(pgstore 필수 사례 skip=0, 영향 취약점 0건). hololive-bot `scripts/ci/pre-push-gate.sh`는 branch push hook에서 실행합니다.
- **logger close 수정의 운영 반영:** shared-go v2.16.1을 릴리스하고 hololive 5개 모듈 pin과 `security.yml`의 shared-go 고정 SHA를 이 PR에서 갱신했습니다. twentyq-bot·chat-bot-go-kakao pin과 iris-stack 포인터는 각 저장소에서 따로 반영합니다. 운영 이미지 반영은 다음 hololive 릴리스·배포 때 이루어집니다.
