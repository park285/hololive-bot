# Admin Dashboard Bot-Only 전환 상태 문서 — 2026-03-09

> **[superseded]** 이 문서는 Rust 백엔드 시절의 시점 기록이며, 이후 Go 재작성으로 여기 적힌 `backend/src/**` 등 파일 경로는 더 이상 유효하지 않습니다. 현재 구조는 `README.md`와 `openapi-pipeline.md`를 참조하세요. 아래 본문은 당시 기록으로 보존합니다.

## 목적

이 문서는 `admin-dashboard`가 현재 monorepo로 편입된 뒤, **bot-only 운영 기준**으로 어떻게 정리되었는지와
남은 확인 포인트를 한 번에 정리하는 SSOT입니다.

---

## 현재 상태 요약

### 완료된 항목

- `admin-dashboard`가 현재 `hololive-bot` monorepo 안으로 편입됨
- backend가 bot-only 기준으로 정리됨
  - game bot 프록시 제거
  - game bot 상태 수집 제거
  - game bot 로그 노출 제거
  - game bot Docker 관리 대상 제거
- frontend에서도 game bot 노출 제거가 반영됨
  - 사이드바/라우트 제거
  - page/component/api/hook/type 잔여물 제거
- 세션/heartbeat 관련 주요 프론트 로직 보강 반영됨

---

## 1. 백엔드 정리 내역

### 1-1. 프록시 라우트 축소

유지:

- `/admin/api/holo/*`

제거:

- `/admin/api/twentyq/*`
- `/admin/api/turtle/*`

관련 파일:

- `backend/internal/server/routes.go`
- `backend/internal/proxy/proxy.go`
- `backend/cmd/admin/main.go`

### 1-2. 시스템 상태/리소스 모니터링 축소

상태 수집 대상에서 제거:

- `twentyq-bot`
- `turtle-soup-bot`

유지 대상:

- `hololive-bot`
- `admin-dashboard`

관련 파일:

- `backend/cmd/admin/main.go`
- `backend/internal/status/status.go`
- `backend/internal/server/handlers_status.go`

### 1-3. 로그 노출 축소

로그 화이트리스트에서 제거:

- `twentyq`
- `turtlesoup`

관련 파일:

- `backend/internal/logs/logs.go`

### 1-4. Docker 관리 대상 축소

Docker 관리 대상 필터에서 제거:

- `twentyq`
- `turtle-soup`

관련 파일:

- `backend/internal/docker/docker.go`

---

## 2. 프론트 정리 내역

### 2-1. 라우트 / 사이드바 / 진입 제거

반영 내용:

- `Game Bots` 그룹 제거
- `twentyq`, `turtlesoup` 라우트 제거
- 사이드바 메뉴와 헤더 라벨에서 game bot 노출 제거

관련 파일:

- `frontend/src/routes/manifest.ts`
- `frontend/src/layouts/AppLayout.tsx` (consumer)
- `frontend/src/App.tsx` (consumer)

### 2-2. game bot 페이지/컴포넌트 제거

삭제된 대상:

- `frontend/src/pages/TwentyQPage.tsx`
- `frontend/src/pages/TurtleSoupPage.tsx`
- `frontend/src/components/gameBots/**`

### 2-3. game bot API / hook / type 제거

삭제/정리된 대상:

- `frontend/src/api/gameBots.ts`
- `frontend/src/hooks/useGameBots.ts`
- `frontend/src/types/gameBots.ts`
- `frontend/src/api/index.ts`에서 관련 export 제거
- `frontend/src/api/queryKeys.ts`에서 game bot query key 제거

### 2-4. 리소스 모니터링 UI 잔여 분기 제거

정리된 대상:

- `frontend/src/config/constants.ts`
  - `twentyq-bot`, `turtle-soup-bot` 색상 제거
- `frontend/src/components/StatsTab.tsx`
  - game bot 아이콘 분기 제거
- `frontend/src/components/dashboard/ServiceStatusGrid.tsx`
  - 잔여 game bot 분기 제거

---

## 3. 세션 / heartbeat 로직 상태

이번 정리에서 확인된 주요 포인트와 반영 상태입니다.

### 3-1. CSRF 경로 문제

이전 의심:

- `authApi.logout`, `authApi.heartbeat`가 generated client를 사용
- `apiClient`의 CSRF 헤더 자동 주입을 우회 가능

현재 반영:

- `frontend/src/api/core.ts`에서 `authApi.login/logout/heartbeat`가 `apiClient` 기반으로 동작
- `X-CSRF-Token` 주입 경로를 타도록 정리됨

### 3-2. 로그인 성공 판정 시점

이전 문제:

- 로그인 응답만 성공하면 바로 `setAuthenticated(true)`

현재 반영:

- 로그인 후 `statusApi.get()`로 보호 API 검증
- 검증 성공 시에만 인증 상태 업데이트

관련 파일:

- `frontend/src/pages/LoginPage.tsx`

### 3-3. idle 정책

이전 문제:

- 10분 무입력 → 사실상 빠른 로그아웃

현재 반영:

- `isIdle=true` 전환 시 다음 interval을 기다리지 않고 즉시 heartbeat 전송
- `idle_rejected` 수신 시 즉시 로그아웃
- backend도 `idle: true` heartbeat에서 `status: "idle", idle_rejected: true`를 반환하고 세션 회전을 막도록 정렬됨
- backend가 `GET /admin/api/auth/session`에 `absolute_expires_at` + `session_policy`를 함께 내려 pre-warning UI를 별도 프론트 작업으로 분리할 수 있게 됨

관련 파일:

- `frontend/src/hooks/useHeartbeat.ts`
- `backend/src/handlers/auth.rs`
- `backend/src/auth/session.rs`
- `docs/SESSION_PREWARNING_BACKEND_CONTRACT.md`

### 3-4. heartbeat 중복 호출/abort

현재 반영:

- `isIdle` 추적을 `ref` 중심으로 단순화
- heartbeat 요청에 `AbortController.signal` 연결
- idle 전환 즉시 heartbeat와 interval heartbeat가 겹치면 이전 요청 abort

관련 파일:

- `frontend/src/hooks/useHeartbeat.ts`

---

## 4. 아직 남은 확인 포인트

### 4-1. 개발 환경 쿠키 정책

다음은 코드 수정과 별개로 환경에서 다시 확인할 필요가 있습니다.

- `FORCE_HTTPS=true`일 때 로컬 HTTP 환경에서 `Secure` 쿠키 저장 여부
- 개발 환경에서는 필요 시:
  - `FORCE_HTTPS=false`
  - 또는 HTTPS dev 환경

관련 파일:

- `backend/internal/config/config.go`
- `backend/internal/auth/auth.go`
- `backend/internal/middleware/csrf.go`
- `frontend/vite.config.ts`

### 4-2. 실제 브라우저/운영 검증

코드상으로는 정리되었지만, 아래는 실제 환경에서 재확인이 필요합니다.

- 로그인 직후 `Set-Cookie(admin_session, csrf_token)` 저장 여부
- 첫 보호 API 호출 성공 여부
- heartbeat 주기 동작 여부
- idle 상태에서 UX가 의도대로 동작하는지

---

## 5. 이번 세션 기준 검증 메모

확인/검증된 항목:

- `cargo test test_idle_heartbeat_ -- --nocapture` (`admin-dashboard/backend`) 통과
- `cargo test test_rotated_heartbeat_includes_absolute_expiry_in_response -- --nocapture` (`admin-dashboard/backend`) 통과
- `cargo test test_absolute_expired_heartbeat_returns_json_and_clears_cookies -- --nocapture` (`admin-dashboard/backend`) 통과
- `cargo fmt --check && cargo test && cargo clippy -- -D warnings` (`admin-dashboard/backend`) 통과
- `npm test` (`admin-dashboard/frontend`) 통과
- `npm run build` (`admin-dashboard/frontend`) 통과
- `npm run lint` (`admin-dashboard/frontend`) 통과

주의:

- `admin-dashboard`는 현재 `deploy/compose/docker-compose.prod.yml`에 서비스로 등록되어 있지 않습니다.
- 따라서 이미지 재빌드는 가능하지만, compose 기반 “서비스 반영”은 별도 배포 경로가 필요합니다.
