# Runtime Split Handoff 2026-04-16

이 문서는 `hololive_execution_audit_and_reimprovement_plan_20260416.md` 기준
runtime split 작업의 **완료 상태**를 기록한다.

## 기준 문서

- authoritative plan: `hololive_execution_audit_and_reimprovement_plan_20260416.md`
- 보조 참고: `hololive_runtime_split_master_plan_20260415.md`
- 보조 참고: `hololive_bot_static_rereview_20260415.md`

## 이번 라운드에서 최종 반영된 항목

다음 장기 조건까지 코드/워크스페이스/문서 기준으로 반영됐다.

- bot가 더 이상 admin route를 코드상으로 소유하지 않음
- bot가 더 이상 alarm-worker runtime/checker/scheduler를 소유하지 않음
- `internal/server` ownership이 `hololive/hololive-admin-api/internal/server` 로 이동됨
- `cmd/admin-api` 가 `hololive/hololive-admin-api/cmd/admin-api` 로 이동됨
- `cmd/alarm-worker` 가 `hololive/hololive-alarm-worker/cmd/alarm-worker` 로 이동됨
- `internal/service/alarm/checker` 가 `hololive/hololive-alarm-worker/internal/service/alarm/checker` 로 이동됨
- `internal/service/alarm/scheduler` 가 `hololive/hololive-alarm-worker/internal/service/alarm/scheduler` 로 이동됨
- `internal/service/system` / `internal/service/trigger` 가 admin-api ownership으로 이동됨
- `internal/service/notification` 이 `hololive-shared/pkg/service/notification` 으로 이동되어
  `hololive-alarm` 별도 모듈 대신 alarm domain의 공용 ownership seam 역할을 수행함
- `internal/service/acl` / `internal/service/activity` 가 `hololive-shared/pkg/service/*` 로 이동됨
- `internal/errors` 가 `hololive-shared/pkg/apperrors` 로 승격됨
- `internal/service/chzzk` / `internal/service/twitch` 가 `hololive-shared/pkg/service/*` 로 이동됨
- `hololive-admin-api/go.mod` / `hololive-alarm-worker/go.mod` 가 생성되고 `go.work` 가 갱신됨
- `docs/current/PROJECT_MAP.md`, entrypoint contract, build/deploy/workflow 표면이 새 모듈 경계에 맞게 갱신됨
- bot module 기준 legacy runtime split import 가 0건으로 고정됨

## 장기 조건 상태

이전 handoff 에 남아 있던 장기 조건은 현재 기준으로 모두 닫혔다.

- [x] `internal/server`는 admin-api ownership 아래로 이동
- [x] `hololive-admin-api` go.mod 추출 완료
- [x] `hololive-alarm-worker` go.mod 추출 완료
- [x] `hololive-alarm` domain library 또는 동등한 소유 모듈 생성
  - 현재 구현: `hololive-shared/pkg/service/notification`
- [x] YouTube ownership의 추가 회수
  - bot는 더 이상 admin/alarm-worker 전용 scheduler/runtime builder를 소유하지 않음

## 현재 기대 디렉터리 상태

```bash
find hololive -maxdepth 2 -type d \( -name 'hololive-admin-api' -o -name 'hololive-alarm-worker' \)
# hololive/hololive-admin-api
# hololive/hololive-alarm-worker
```

## 현재 확인 grep

다음 grep 는 현재 0건이어야 정상이다.

```bash
rg -n "BOT_ADMIN_ENABLED|cfg\.Bot\.AdminEnabled|AdminEnabled" hololive/hololive-kakao-bot-go hololive/hololive-shared docker-compose.prod.yml
rg -n "InitCoreInfrastructure\(" hololive/hololive-kakao-bot-go
rg -n "GetYouTubeScheduler|deps\.Scheduler|Scheduler\s+youtube\.Scheduler" hololive/hololive-kakao-bot-go/internal/app hololive/hololive-kakao-bot-go/internal/bot -g '!**/*_test.go'
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/service/(acl|activity|chzzk|twitch|notification)' -g '*.go'
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/errors' -g '*.go'
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/(server|service/system|service/trigger|service/alarm/(checker|scheduler))' -g '*.go'
```

## 이번 라운드 검증 기록

실행했고 통과한 명령:

```bash
go test . -run TestRuntimeSplitStandaloneModulesContract
make -C hololive/hololive-kakao-bot-go fmt lint
make -C hololive/hololive-kakao-bot-go test
go test ./...                    # hololive/hololive-admin-api
go test ./...                    # hololive/hololive-alarm-worker
go test ./...                    # hololive/hololive-kakao-bot-go
go test ./...                    # hololive/hololive-shared
./scripts/architecture/ci-boundary-gate.sh
docker compose -f docker-compose.prod.yml config --no-interpolate
./build-all.sh --no-bump --build-only hololive-admin-api hololive-alarm-worker
```

## 다음 컨텍스트에서 할 일

runtime split 자체는 완료 상태다.
다음 작업은 새 모듈을 실제로 배포/재기동하거나 운영 smoke 를 실행하는 경우에만 진행하면 된다.
