# Runtime Split PR-07 Blockers 2026-04-16

이 문서는 `8db98f05557a7a62bb8f3a56ad92ac551634dafc` 이후 상태에서
PR-07 멀티모듈 추출을 다음 컨텍스트에서 바로 이어가기 위한
실행 메모다.

## 전제

이전 라운드에서 PR-01~PR-06 성격의 선행 정리는 이미 반영됐다.
현재 source of truth는 아래다.

- `docs/current/RUNTIME_SPLIT_HANDOFF_20260416.md`
- `hololive_execution_audit_and_reimprovement_plan_20260416.md`

## 이번 추가 점검에서 확인된 실제 blocker

### 1. `hololive-admin-api` 추출의 즉시 blocker

`internal/server`를 새 admin-api 모듈로 옮기려면, 먼저 아래 두 패키지가
bot-internal ownership에서 빠져야 한다.

- `hololive/hololive-kakao-bot-go/internal/service/acl`
- `hololive/hololive-kakao-bot-go/internal/service/activity`

이유:

- `internal/server/api.go`
- `internal/server/api_room.go`
- 관련 테스트들

이 아직 위 두 패키지를 직접 import 한다.
새 모듈 `hololive-admin-api`는 기존 `hololive-kakao-bot-go/internal/...`
를 import 할 수 없으므로, 이 둘을 그대로 둔 상태에서는
`internal/server` 이동이 막힌다.

### 2. `hololive-alarm-worker` 추출의 즉시 blocker

alarm worker 쪽은 아래 패키지가 아직 bot-internal이다.

- `hololive/hololive-kakao-bot-go/internal/service/chzzk`
- `hololive/hololive-kakao-bot-go/internal/service/twitch`

그리고 두 클라이언트는 내부 error 타입에도 기대고 있다.

- `hololive/hololive-kakao-bot-go/internal/errors`

즉, 새 모듈 `hololive-alarm-worker`가 checker/scheduler/runtime builder를
가져가려면 먼저 다음 ownership 정리가 필요하다.

- `chzzk` / `twitch`를 shared 또는 별도 public library ownership으로 이동
- `internal/errors` 의 필요한 타입을 public package로 승격

## 추천 실행 순서

다음 컨텍스트에서는 아래 순서를 권장한다.

### Step 1. admin-api extraction blocker 제거

먼저 thin shared service 2개를 이동한다.

- `internal/service/acl` -> `hololive-shared/pkg/service/acl`
- `internal/service/activity` -> `hololive-shared/pkg/service/activity`

그 다음 import 를 전부 갱신하고 검증한다.

권장 검증:

```bash
make -C hololive/hololive-kakao-bot-go fmt lint
make -C hololive/hololive-kakao-bot-go test
go test ./hololive/hololive-shared/...
```

### Step 2. `hololive-admin-api` 모듈 추출

thin shared service 이동 후, 아래를 새 모듈로 옮긴다.

- `hololive/hololive-kakao-bot-go/cmd/admin-api`
- `hololive/hololive-kakao-bot-go/internal/server`
- `hololive/hololive-kakao-bot-go/internal/service/system`
- `hololive/hololive-kakao-bot-go/internal/service/trigger`
- admin runtime 관련 `internal/app` 파일
- admin API router 관련 `internal/app/http` 파일

주의:

- bot는 더 이상 admin route를 쓰지 않으므로, bot 쪽에는 `ProvideBotRouter`만 남기고
  admin-specific router 파일은 새 모듈 ownership으로 넘기는 방향이 자연스럽다.

### Step 3. alarm-worker extraction blocker 제거

다음 public ownership 이동이 필요하다.

- `internal/errors` 중 `chzzk`/`twitch` 가 필요한 타입 -> `hololive-shared/pkg/apperrors`
- `internal/service/chzzk` -> `hololive-shared/pkg/service/chzzk`
- `internal/service/twitch` -> `hololive-shared/pkg/service/twitch`

### Step 4. `hololive-alarm-worker` 모듈 추출

그 다음 아래를 새 모듈로 옮긴다.

- `hololive/hololive-kakao-bot-go/cmd/alarm-worker`
- `internal/service/alarm/checker`
- `internal/service/alarm/scheduler`
- alarm-worker runtime/builder/bootstrap 관련 `internal/app` 파일

### Step 5. 필요 시 `hololive-alarm` domain library

worker/admin/bot 공통 alarm domain ownership이 여전히 커지면
그때 `hololive-alarm` 분리를 진행한다.

## 바로 재현 가능한 grep 메모

### admin-api blocker grep

```bash
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/service/acl' -g '*.go'
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/service/activity' -g '*.go'
```

### alarm-worker blocker grep

```bash
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/service/(chzzk|twitch)' -g '*.go'
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/errors' -g '*.go'
```

## 현재 상태 요약

- working tree 는 clean 이어야 한다.
- 기준 커밋은 `8db98f05557a7a62bb8f3a56ad92ac551634dafc` 이다.
- 다음 컨텍스트의 첫 실작업은 **`acl` / `activity` shared 이동**이 가장 안전하다.
