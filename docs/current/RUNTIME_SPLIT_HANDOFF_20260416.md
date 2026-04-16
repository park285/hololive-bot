# Runtime Split Handoff 2026-04-16

이 문서는 `hololive_execution_audit_and_reimprovement_plan_20260416.md` 기준 실행 상태를 다음 컨텍스트에서 바로 이어받기 위한 handoff 문서다.

## 기준 문서

- authoritative plan: `hololive_execution_audit_and_reimprovement_plan_20260416.md`
- 보조 참고: `hololive_runtime_split_master_plan_20260415.md`
- 보조 참고: `hololive_bot_static_rereview_20260415.md`

## 이번 라운드에서 닫힌 항목

다음은 코드/설정/문서 기준으로 이미 반영됐다.

- bot가 더 이상 admin route를 코드상으로 제공하지 않음
- bot가 더 이상 alarm scheduler lifecycle을 소유하지 않음
- `BOT_ADMIN_ENABLED`가 코드와 Compose에서 제거됨
- bot의 `NOTIFICATION_SCHEDULER_ROLE`이 `off`로 정렬됨
- `BuildAdminAPIRuntime` / `BuildAlarmWorkerRuntime`가 더 이상 `InitCoreInfrastructure(...)`를 사용하지 않음
- runtime scheduler constructor가 `*notification.AlarmService` 대신 `domain.AlarmCRUD` 경계로 축소됨
- alarm-worker config subscriber가 giant infra 대신 `cache + AlarmCRUD`만 받도록 축소됨
- bot-side YouTube scheduler field/accessor coupling이 제거됨
- stale 문서 3종에 historical/deprecated 배너가 추가됨
- focused test / broader test / lint / compose validation이 모두 통과함

## 아직 완전히 닫히지 않은 항목

문서 전체 기준으로는 아직 완료가 아니다. 아래 항목이 남아 있다.

### 1. `internal/server` ownership 이동 미완료

현재 상태:

```bash
find hololive/hololive-kakao-bot-go/internal -maxdepth 2 -type d -name server
# hololive/hololive-kakao-bot-go/internal/server
```

즉, 체크리스트의

- `internal/server`는 최소한 admin-api ownership 아래로 이동한다

는 아직 미완료다.

### 2. PR-07 멀티모듈 추출 미착수

아직 없는 경로:

```bash
find hololive -maxdepth 2 -type d \( -name 'hololive-admin-api' -o -name 'hololive-alarm-worker' -o -name 'hololive-alarm' \)
# no output
```

따라서 아래는 아직 안 됐다.

- `hololive-admin-api/` 모듈 추출
- `hololive-alarm-worker/` 모듈 추출
- `hololive-alarm/` domain library 도입
- `go.work` 갱신

### 3. 장기 조건 미완료

아직 남아 있는 장기 조건:

- `hololive-alarm` domain library 또는 동등한 소유 모듈 생성
- admin-api / alarm-worker go.mod 추출 완료
- YouTube ownership의 추가 회수

## 다음 컨텍스트에서 바로 할 일

순서는 authoritative plan의 의도를 유지한다.

1. `internal/server`를 admin-api ownership으로 이동할 최소 모듈 경계 설계 확정
2. `hololive/hololive-admin-api/` 모듈 생성
3. `cmd/admin-api` + admin runtime 관련 코드 + `internal/server` 이동
4. `hololive/hololive-alarm-worker/` 모듈 생성
5. `cmd/alarm-worker` + alarm checker/scheduler/runtime builder 이동
6. 필요 시 `hololive-alarm/` domain library 분리
7. 마지막에 `go.work` 갱신

## 다음 컨텍스트 시작 전 확인 grep

다음 grep는 현재 0건이어야 정상이다.

```bash
rg -n "BOT_ADMIN_ENABLED|cfg\.Bot\.AdminEnabled|AdminEnabled" hololive/hololive-kakao-bot-go hololive/hololive-shared docker-compose.prod.yml
rg -n "InitCoreInfrastructure\(" hololive/hololive-kakao-bot-go
rg -n "GetYouTubeScheduler|deps\.Scheduler|Scheduler\s+youtube\.Scheduler" hololive/hololive-kakao-bot-go/internal/app hololive/hololive-kakao-bot-go/internal/bot -g '!**/*_test.go'
```

주의:

- historical 문서에는 `alarm-dispatcher`, `30002`, `hololive-admin/`, `hololive-alarm/` 문자열이 남아 있을 수 있다.
- 이건 PR-06에서 배너로 처리한 historical state이며, 현재 source of truth는 아니다.

## 이번 라운드 검증 기록

실행했고 통과한 명령:

```bash
make -C hololive/hololive-kakao-bot-go fmt lint
make -C hololive/hololive-kakao-bot-go test
go test ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-shared/...
docker compose -f docker-compose.prod.yml config --no-interpolate
git diff --check
```

## 커밋 이후 기대 상태

이 handoff 문서가 포함된 커밋은 다음 의미를 가진다.

- PR-01~PR-06 성격의 선행 정리 작업은 반영 완료
- 문서 전체 기준의 최종 종료는 아님
- 다음 작업자는 PR-07 멀티모듈 추출과 ownership 이동만 집중하면 됨
