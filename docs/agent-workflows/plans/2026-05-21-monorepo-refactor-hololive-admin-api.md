# 2026-05-21 — hololive-admin-api refactor (Phase 2.C.3)

Sub-plan of `2026-05-21-monorepo-refactor-master.md`.

## Goal

`internal/server/internal/api/api_member.go` OVER (430/430) 해소, lifecycle/runtime 테스트 0% 영역 보강, 도메인 핸들러 wrapper 패턴 단일화, 에러 응답 보일러플레이트 추출.

## Inventory

`docs/agent-workflows/plans/_inventory-2026-05-21/02-hololive-admin-api.md`

## Target work

LOC / 함수 budget:
- `internal/server/internal/api/api_member.go` 430/430 OVER — `UpdateChannelID`/`UpdateMemberName` 동형 핸들러를 factory 로 통합 후 파일 분리.
- `internal/app/build_runtime.go` 387/UNLISTED — `BuildAdminAPIRuntime` 9단계 sequential 빌더를 functional options 또는 builder chain 으로.
- `internal/server/internal/api/settings_handler.go` 363 — 인터페이스 그룹/구조체 선언 보일러플레이트 분리.
- `internal/server/internal/api/api_youtube_ops.go` 347 — youtube 관련 핸들러 split.
- `internal/server/internal/api/api_auth.go` 318 — AuthHandler 와 도메인 wrapper 패턴 통일.

테스트 보강:
- `internal/app/` 7 prod / 3 test — `runtime_admin_api.go`, `_shutdown.go`, `_runner.go`, `settings_applier.go`, `http/{registration,middleware}.go` 테스트.
- `internal/app/runtime/` 2 prod / 0 test — `lifecycle.go` (215), `http_server.go` (77).
- `internal/server/internal/api/` 미커버 핸들러: `api_alarm`, `api_majorevent`, `api_deps`, `api_domains`, `oauth_proxy`. catch-all `api_low_coverage_test.go` 의 분량을 도메인별 테스트로 분리.

네이밍 단일화:
- 도메인 핸들러 wrapper 패턴: `MemberAPIHandler`/`AlarmAPIHandler`/`RoomAPIHandler` 의 anonymous embed 방식 통일 또는 명시 위임으로 단일화. `AuthHandler` 와 `SettingsHandler` 도 같은 wrapper 패턴으로 정렬.
- `api.APIHandler` 의 package-name 중복 — `Handler` 또는 `AdminHandler` 로 rename(영향도 확인 후).

중복 → 추출:
- `respondWithError(c, err, ...)` 통합 (30+ 호출부 일괄).
- `UpdateChannelID`/`UpdateMemberName` 의 parse → bind → repo → cache refresh → activity log factory.
- HTTP server lifecycle: Phase 2.B.3 helper.
- CORS/origin validation `internal/app/http/middleware.go:35–63` → `hololive-shared/pkg/server/middleware` 와 단일화.

## File map

```
internal/server/internal/api/                # api_member 분할, settings_handler 그룹 분리, respondWithError 도입
internal/app/                                 # build_runtime functional options, settings_applier 테스트
internal/app/runtime/                         # lifecycle + http_server 테스트, 2.B.3 helper 적용
internal/app/http/                            # middleware 통합 (hololive-shared 와)
```

## Validation

```bash
./build-all.sh --no-bump
go build ./hololive/hololive-admin-api/...
go test  ./hololive/hololive-admin-api/...
./scripts/architecture/ci-boundary-gate.sh
```

## Stop rules

- 도메인 핸들러 wrapper 패턴 변경이 외부 라우트/응답 shape 에 영향을 주면 stop.
- `api.APIHandler` rename 의 호출부가 모듈 외(예: 통합 테스트, 다른 binary) 에 존재하면 별도 PR 로 분리.
- CORS/origin 통합이 admin-api 외 다른 서버의 동작에 영향을 주면 stop.

## Out of scope

- Admin API 응답 JSON shape 변경.
- 인증 흐름의 정책 변경(세션 TTL, CSRF mode 등) — admin-dashboard sub-plan 참조.
- OAuth proxy 동작 변경.
