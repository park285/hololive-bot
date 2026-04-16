# Runtime Split PR-07 Blockers 2026-04-16

이 문서는 PR-07 멀티모듈 추출 과정에서 확인됐던 blocker 와
그 해소 결과를 기록한다.

## 최종 상태

현재 기준으로 PR-07 blocker 는 모두 해소됐다.

### admin-api 쪽

- `internal/service/acl` -> `hololive-shared/pkg/service/acl`
- `internal/service/activity` -> `hololive-shared/pkg/service/activity`
- `internal/server` -> `hololive/hololive-admin-api/internal/server`
- `cmd/admin-api` -> `hololive/hololive-admin-api/cmd/admin-api`
- `internal/service/system` / `internal/service/trigger` -> `hololive/hololive-admin-api/internal/service/*`

### alarm-worker 쪽

- `internal/errors` -> `hololive-shared/pkg/apperrors`
- `internal/service/chzzk` -> `hololive-shared/pkg/service/chzzk`
- `internal/service/twitch` -> `hololive-shared/pkg/service/twitch`
- `internal/service/notification` -> `hololive-shared/pkg/service/notification`
- `internal/service/alarm/checker` -> `hololive/hololive-alarm-worker/internal/service/alarm/checker`
- `internal/service/alarm/scheduler` -> `hololive/hololive-alarm-worker/internal/service/alarm/scheduler`
- `cmd/alarm-worker` -> `hololive/hololive-alarm-worker/cmd/alarm-worker`

## ownership 결론

별도 `hololive-alarm` 모듈을 추가로 만들지 않고,
`hololive-shared/pkg/service/notification` 을 alarm domain의 공용 ownership seam 으로 승격했다.
이 경계로 인해 bot / admin-api / alarm-worker 모두가 같은 alarm service ownership 을 공유하면서도
기존 bot internal import 없이 독립 모듈 구성이 가능해졌다.

## 워크스페이스 상태

- `hololive/hololive-admin-api/go.mod` 생성 완료
- `hololive/hololive-alarm-worker/go.mod` 생성 완료
- `go.work` 에 두 모듈이 등록됨
- `docs/current/PROJECT_MAP.md` 와 entrypoint contract 가 새 경계를 반영함

## 검증 증거

다음 검증이 통과했다.

```bash
go test . -run TestRuntimeSplitStandaloneModulesContract
go test ./...                    # hololive/hololive-admin-api
go test ./...                    # hololive/hololive-alarm-worker
go test ./...                    # hololive/hololive-kakao-bot-go
go test ./...                    # hololive/hololive-shared
./scripts/architecture/ci-boundary-gate.sh
docker compose -f docker-compose.prod.yml config --no-interpolate
./build-all.sh --no-bump --build-only hololive-admin-api hololive-alarm-worker
```

그리고 다음 grep 는 0건이다.

```bash
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/service/(acl|activity|chzzk|twitch|notification)' -g '*.go'
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/errors' -g '*.go'
rg -n 'github.com/kapu/hololive-kakao-bot-go/internal/(server|service/system|service/trigger|service/alarm/(checker|scheduler))' -g '*.go'
```

## 결론

PR-07 blocker 문서는 더 이상 “다음에 해야 할 일” 문서가 아니다.
현재는 멀티모듈 추출 완료 후 상태 기록으로 취급하면 된다.
