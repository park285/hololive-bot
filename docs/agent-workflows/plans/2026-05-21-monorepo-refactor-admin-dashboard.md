# 2026-05-21 — admin-dashboard refactor (Phase 2.C.7 + 2.C.8)

Sub-plan of `2026-05-21-monorepo-refactor-master.md`. **본 sub-plan 은 backend(Rust) 와 frontend(TS) 를 두 phase 로 분리**해 진행한다. 같은 세션 내 동시 진행은 금지.

- Phase 2.C.7 — admin-dashboard backend (Rust)
- Phase 2.C.8 — admin-dashboard frontend (TS/React)

## Goal

backend: `handlers/auth.rs` 1308/1320 NEAR 해소 + auth/session/middleware 테스트 보강 + 핸들러 추출.
frontend: `DockerContainerList.tsx` 329 라인 OVER 해소, hook/모달/액션 패턴 통합, 핵심 page 테스트 추가.

## Inventory

`docs/agent-workflows/plans/_inventory-2026-05-21/08-admin-dashboard.md`

## Target work — Phase 2.C.7 (backend)

LOC / 함수:
- `backend/src/handlers/auth.rs` 1308/1320 NEAR — `handle_login`(~97), `handle_session_status`(~122), `handle_heartbeat`(~71) 를 단계별 함수 + 별도 파일로 분해(`auth/login.rs`, `auth/session.rs`, `auth/heartbeat.rs`).
- `backend/src/auth/session.rs` 701/720 NEAR — `Session::rotate()`, `SessionRefreshResult` enum, `is_absolutely_expired_at()` 추출 + 테스트.
- `backend/src/config.rs` 389/400 NEAR — 도메인별 config 분리.
- `backend/src/auth/middleware.rs` 386/UNLISTED — `extract_cookie` edge case 테스트, set_*_cookie 통합.
- `backend/src/error.rs` 347/UNLISTED — `AppError::into_response()` + `ErrorResponse::from_value()` 의 `Result` alias + `?` ergonomics.

테스트 보강(backend):
- `session.rs` rotate/refresh/expiry 미커버 분기.
- `middleware.rs` `extract_cookie` 잘못된 쿠키/구분자 누락 케이스.
- `config/security.rs` CORS allow-list, CSRF mode transition.
- `holo/handlers_{commands,queries}.rs` upstream 5xx + malformed response 통합 테스트.

네이밍 단일화(backend):
- `holo/handlers.rs` (73B barrel) 의 책임 명확화 — 단순 re-export 면 doc 명시, 통합이면 파일 통합.
- Axum State extractor 인자명 `State(state)` vs `State(app_state)` 통일.
- `ValkeySessionStore` ↔ docs/주석의 "Redis" 잔존 텍스트 정리.

중복 추출(backend):
- 인증 핸들러 boilerplate (`State extract + session validate + AppError 응답`) — macro 또는 axum extractor 로.
- Cookie setters (`set_session_cookie`, `set_csrf_cookie`, `set_clear_cookie`) — `CookieBuilder`.
- Status 핸들러 (`handle_aggregated_status`, `handle_system_stats_stream`, `handle_docker_health`) — status trait 또는 middleware.

## Target work — Phase 2.C.8 (frontend)

LOC / 컴포넌트:
- `frontend/src/components/settings/DockerContainerList.tsx` 329 라인 OVER — 모달/액션/리스트로 분할(`DockerContainerList.tsx`, `DockerContainerActions.tsx`, `DockerContainerConfirmModal.tsx`).
- `frontend/src/features/alarms/components/AlarmGroups.tsx` ~200 — 메모/렌더 helper 분리.
- `frontend/src/hooks/useMembersPage.ts` ~238 — query/mutation/state 분리.

테스트 보강(frontend):
- `DockerContainerList` 액션 확정 흐름 + toast 에러 테스트.
- `useHeartbeat` 네트워크 실패 시 timeout/recovery 테스트.
- `useWebSocket` 재연결 backoff + malformed message 테스트.
- `MembersPage`/`MilestonesPage`/`StreamsPage` 페이지-레벨 테스트.

네이밍 단일화(frontend):
- Hook 카테고리: factory(`useMemberMutations`) vs effect(`useHeartbeat`) vs orchestration(`useMembersPage`) — 명명 컨벤션 doc 후 정렬.
- `authApi.login()` (suffix) vs backend `handle_login()` (prefix) — symmetry 결정.

중복 추출(frontend):
- `useMutationWithToast()` — `useMutation` + `toast.error()` + `invalidateQueries` 패턴 흡수.
- `<FeaturePage>` wrapper — Members/Streams/Milestones/Alarms/Rooms 공용 layout.

## File map

```
admin-dashboard/backend/src/handlers/                  # auth.rs 분할
admin-dashboard/backend/src/auth/                      # session/middleware 테스트 + 헬퍼
admin-dashboard/backend/src/config/                    # 분할 + security 테스트
admin-dashboard/backend/src/error.rs                   # Result alias / from_value 정리
admin-dashboard/backend/src/holo/                      # handlers.rs 책임 정리
admin-dashboard/frontend/src/components/settings/      # DockerContainerList 분할
admin-dashboard/frontend/src/features/alarms/          # AlarmGroups 분리
admin-dashboard/frontend/src/hooks/                    # useMembersPage 분할, useMutationWithToast
admin-dashboard/frontend/src/components/layout/        # FeaturePage wrapper
docs/architecture/file-loc-thresholds.txt              # DockerContainerList 등 새 ceiling 등록
```

## Validation

Phase 2.C.7 (backend):
```bash
( cd admin-dashboard/backend && cargo build --release && cargo test )
./build-all.sh --no-bump
./scripts/architecture/ci-boundary-gate.sh
```

Phase 2.C.8 (frontend):
```bash
( cd admin-dashboard/frontend && npm run typecheck )
( cd admin-dashboard/frontend && npm run lint )
( cd admin-dashboard/frontend && npm run test -- --run )
( cd admin-dashboard/frontend && npm run build )
```

## Stop rules

- auth.rs 분할 도중 세션 회전(rotate) 의미 변화 가능성이 발견되면 stop, 회귀 테스트 우선.
- DockerContainerList 분할이 사용자 시각적 UX 를 바꾸면 stop.
- CookieBuilder 도입이 외부에 노출되는 Set-Cookie 헤더 값 변경을 유발하면 stop.

## Out of scope

- 세션/CSRF 정책 의미 변경 (TTL, rotation interval, absolute expiry).
- 인증 흐름의 외부 사용자 동작 변경 (로그인 redirect, OAuth 흐름).
- 백엔드↔프론트엔드 API 응답 shape 변경.
- generated 코드(`frontend/src/api/generated/*`) 수정.
