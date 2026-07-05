# Session Pre-warning Frontend Handoff

이 문서는 관리자 대시보드 세션 pre-warning UX를 프론트엔드 팀이 **이 문서만 보고 바로 구현**할 수 있도록 정리한 handoff 문서입니다.

범위:

- 백엔드 계약: 구현 완료 및 동시성/입력검증 방어 반영
- 프론트엔드 모달/상태/UI: 별도 구현 필요

이 문서의 목적은 아래 두 가지입니다.

1. 서버가 이미 제공하는 계약을 정확히 정의한다.
2. 현재 프론트 구조에서 어디를 어떻게 바꾸면 되는지 구현 절차를 제공한다.

---

## 1. 현재 상태 요약

이미 완료된 것:

- `GET /admin/api/auth/session`에서 절대 만료 시각과 warning 정책을 제공함
- `POST /admin/api/auth/heartbeat`가 idle/rotation/absolute-expired 계약을 정확히 반환함
- `idle: true` heartbeat는 세션 회전을 유발하지 않음
- `idle_rejected=true`면 프론트는 즉시 logout 하도록 현재 기본 heartbeat 훅이 정렬되어 있음

아직 구현되지 않은 것:

- idle pre-warning 모달 UI
- absolute timeout pre-warning 모달 UI
- 위 두 모달의 표시/해제/연장 액션 상태 관리

즉, **백엔드 계약은 세션 회전 동시성, malformed heartbeat 방어, 절대 만료 전달까지 반영된 상태이며, 남은 일은 프론트 UI/상태 구현뿐**입니다.

---

## 2. 서버 계약

## 2-1. `GET /admin/api/auth/session`

인증 부트스트랩 시 호출합니다.

응답 예시:

```json
{
  "status": "ok",
  "authenticated": true,
  "username": "admin",
  "absolute_expires_at": 1735568988,
  "session_policy": {
    "heartbeat_interval_ms": 300000,
    "idle_timeout_ms": 600000,
    "idle_warning_timeout_ms": 540000,
    "idle_session_ttl_ms": 10000,
    "absolute_warning_window_ms": 300000
  }
}
```

필드 의미:

- `absolute_expires_at`
  - Unix timestamp seconds
  - 절대 만료 시각
- `session_policy.heartbeat_interval_ms`
  - 정기 heartbeat 간격
- `session_policy.idle_timeout_ms`
  - idle 확정 시각
- `session_policy.idle_warning_timeout_ms`
  - idle pre-warning 모달 표시 시작 시각
- `session_policy.idle_session_ttl_ms`
  - idle 확정 후 서버가 세션 TTL을 줄이는 시간
- `session_policy.absolute_warning_window_ms`
  - 절대 만료 pre-warning 모달을 띄우기 시작할 기준 창

### 프론트 타입

현재 `frontend/src/api/core.ts`와 generated client에 아래 타입이 이미 반영되어 있습니다.

```ts
type SessionPolicy = {
  heartbeat_interval_ms: number
  idle_timeout_ms: number
  idle_warning_timeout_ms: number
  idle_session_ttl_ms: number
  absolute_warning_window_ms: number
}

type SessionStatusResponse = {
  status: string
  authenticated: boolean
  username: string
  absolute_expires_at: number
  session_policy: SessionPolicy
}
```

## 2-2. `POST /admin/api/auth/heartbeat`

### 활성 상태 응답

세션이 회전되지 않은 일반 heartbeat도 절대 만료 시각을 반환합니다.

```json
{
  "status": "ok",
  "absolute_expires_at": 1735568988
}
```

세션 회전이 발생했거나 이미 회전된 구 세션으로 heartbeat가 늦게 도착한 경우에는 새 세션 쿠키와 CSRF 토큰이 함께 내려옵니다.

```json
{
  "status": "ok",
  "rotated": true,
  "absolute_expires_at": 1735568988,
  "csrf_token": "<new-token>"
}
```

주의:

- `rotated=true`면 새 세션 기준 세션 쿠키와 CSRF 토큰이 내려옵니다.
- `absolute_expires_at`는 절대 만료 시각입니다.
- malformed heartbeat body는 세션 연장으로 간주하지 않고 HTTP `400`으로 거절합니다.

### idle 확정 응답

```json
{
  "status": "idle",
  "idle_rejected": true
}
```

주의:

- 서버는 이 응답 직전에 세션 TTL을 `idle_session_ttl_ms`로 단축합니다.
- 프론트는 이 응답을 받으면 **즉시 logout** 해야 합니다.
- 현재 `useHeartbeat.ts`는 이미 그렇게 동작합니다.

### 절대 만료 응답

HTTP `401`

```json
{
  "error": "Session expired",
  "absolute_expired": true
}
```

주의:

- 서버는 세션/CSRF 쿠키도 정리합니다.
- 프론트는 즉시 재로그인 플로우로 전환해야 합니다.

---

## 3. 현재 프론트 구조에서 수정할 파일

필수 수정 대상:

- `frontend/src/hooks/useAuthBootstrap.ts`
- `frontend/src/stores/authStore.ts`
- `frontend/src/hooks/useActivityDetection.ts`
- `frontend/src/hooks/useHeartbeat.ts`
- `frontend/src/App.tsx`

신규 추가 권장:

- `frontend/src/stores/sessionWarningStore.ts`
- `frontend/src/components/auth/SessionIdleWarningModal.tsx`
- `frontend/src/components/auth/SessionAbsoluteWarningModal.tsx`
- `frontend/src/hooks/useSessionWarnings.ts`

선택:

- `frontend/src/lib/time.ts` 또는 기존 util에 남은 시간 계산 유틸 추가

---

## 4. 권장 구현 방식

## 4-1. 상태 모델

프론트는 아래 상태를 분리해 관리하면 됩니다.

```ts
type SessionWarningState = {
  absoluteExpiresAt: number | null
  policy: SessionPolicy | null
  lastActivityAtMs: number
  idleWarningOpen: boolean
  absoluteWarningOpen: boolean
  idleWarningDismissedAtMs: number | null
}
```

핵심 규칙:

- `absoluteExpiresAt`와 `policy`는 로그인 bootstrap 시 서버에서 받는다.
- `lastActivityAtMs`는 활동 감지 시 갱신한다.
- `idleWarningOpen`은 `now - lastActivityAtMs >= idle_warning_timeout_ms`가 되면 `true`
- `absoluteWarningOpen`은 `absolute_expires_at - now <= absolute_warning_window_ms`가 되면 `true`
- `idleWarningDismissedAtMs`는 UX적으로 “나중에 다시 보기”를 넣을 때만 필요하다. 없으면 생략 가능하다.

## 4-2. idle pre-warning 타이밍

기준:

- `idle_warning_timeout_ms = 9분`
- `idle_timeout_ms = 10분`

의미:

- 9분 무활동: idle warning 모달 오픈
- 10분 무활동: `sendHeartbeat(true)` 호출
- 서버 응답 `idle_rejected=true`: 즉시 logout

### 중요한 점

현재 `useActivityDetection.ts`는 탭 간 활동 브로드캐스트를 이미 하고 있습니다.
따라서 **warning 모달도 같은 activity reset 신호를 기준으로 닫히게** 만들면 됩니다.

즉:

- 다른 탭 활동으로 `resetTimerInternal()`이 호출되면
- 이 탭의 idle warning 모달도 닫혀야 합니다.

## 4-3. absolute pre-warning 타이밍

기준:

- `absolute_warning_window_ms = 5분`

의미:

- 절대 만료까지 5분 이하가 되면 absolute warning 모달 오픈
- 이 경고는 사용자가 활동하더라도 사라지면 안 됩니다.
- idle warning과 달리 absolute warning은 “시간 기반 강제 재로그인 예정” 안내입니다.

추천 UX:

- 남은 시간 카운트다운 표시
- “확인”만 가능
- “연장” 버튼은 제공하지 않음
  - 절대 만료는 연장 불가이기 때문

---

## 5. 실제 구현 절차

## 5-1. `useAuthBootstrap.ts`

현재는 인증 여부만 저장합니다.

여기에 추가할 것:

1. `authApi.getSession()` 응답에서
   - `absolute_expires_at`
   - `session_policy`
   를 store에 저장
2. 인증 실패 시 warning 관련 상태도 초기화

의사 코드:

```ts
const session = await authApi.getSession()
setAuthenticated(session.authenticated)
setSessionPolicy(session.session_policy)
setAbsoluteExpiresAt(session.absolute_expires_at)
markSessionActivity(Date.now())
```

## 5-2. `authStore.ts` 또는 신규 `sessionWarningStore.ts`

추가 액션:

```ts
setSessionPolicy(policy)
setAbsoluteExpiresAt(unixSeconds)
markSessionActivity(nowMs)
openIdleWarning()
closeIdleWarning()
openAbsoluteWarning()
closeAbsoluteWarning()
resetSessionWarnings()
```

로그아웃 시 반드시 같이 호출:

```ts
resetSessionWarnings()
```

## 5-3. `useActivityDetection.ts`

현재 훅은 boolean `isIdle`만 반환합니다.

프론트 구현 편의를 위해 아래 둘 중 하나를 추천합니다.

### 선택지 A: 최소 변경

- 기존 `boolean isIdle` 유지
- activity 이벤트가 발생할 때 store에 `markSessionActivity(Date.now())`만 추가

### 선택지 B: 더 나은 구조

- `isIdle` 외에 `lastActivityAtMs`도 반환

예:

```ts
return {
  isIdle,
  lastActivityAtMs,
}
```

이번 작업에서는 **선택지 A**가 더 작은 변경입니다.

## 5-4. 신규 `useSessionWarnings.ts`

이 훅에서 warning 모달 open/close를 계산하는 것이 가장 깔끔합니다.

해야 할 일:

1. `policy`, `absoluteExpiresAt`, `lastActivityAtMs`, `isAuthenticated` 구독
2. 짧은 interval 또는 `setTimeout`으로 다음 warning 시점 예약
3. 조건 충족 시 store에 `openIdleWarning`, `openAbsoluteWarning`
4. activity reset 시 idle warning 닫기
5. logout 시 둘 다 닫기

추천 규칙:

- idle warning은 activity 발생 시 닫기
- absolute warning은 activity로 닫지 않기
- 두 모달이 동시에 뜰 수 있으면 absolute warning 우선

## 5-5. `useHeartbeat.ts`

현재 구현은 아래를 이미 만족합니다.

- idle 전환 즉시 heartbeat 전송
- `idle_rejected` 즉시 logout
- absolute expired 즉시 logout

추가로 하면 좋은 것:

- rotated heartbeat 응답에 `absolute_expires_at`가 오면 store 갱신

예:

```ts
if (response.absolute_expires_at) {
  setAbsoluteExpiresAt(response.absolute_expires_at)
}
```

이 한 줄이 있으면 absolute warning countdown이 회전 응답과도 항상 동기화됩니다.

## 5-6. `App.tsx`

`ProtectedRoute` 안에서 이미

- `useActivityDetection(...)`
- `useHeartbeat(isIdle)`

를 호출하고 있습니다.

여기에 추가할 것:

```tsx
useSessionWarnings(isIdle)
```

그리고 공통 레이아웃 또는 최상단에:

```tsx
<SessionIdleWarningModal />
<SessionAbsoluteWarningModal />
```

---

## 6. UX 명세

## 6-1. Idle warning modal

표시 조건:

- 마지막 활동 이후 `idle_warning_timeout_ms` 이상
- 아직 `idle_timeout_ms` 미만
- 로그인 상태

권장 카피:

- 제목: `곧 자동 로그아웃됩니다`
- 본문: `활동이 없어 잠시 후 로그아웃됩니다. 계속 사용하려면 연장하세요.`
- 보조 문구: `약 1분 뒤 자동 로그아웃됩니다.`

권장 버튼:

- `세션 연장`
- `로그아웃`

버튼 동작:

### 세션 연장

1. `markSessionActivity(Date.now())`
2. `closeIdleWarning()`
3. `sendHeartbeat(false)` 또는 동등한 연장 API 호출

### 로그아웃

1. `authApi.logout()` best-effort
2. `logout()`

## 6-2. Absolute warning modal

표시 조건:

- `absolute_expires_at - now <= absolute_warning_window_ms`
- 로그인 상태

권장 카피:

- 제목: `세션이 곧 만료됩니다`
- 본문: `보안을 위해 세션이 곧 만료됩니다. 진행 중인 작업을 저장해주세요.`
- 보조 문구: `절대 만료 시간은 연장되지 않습니다.`

권장 버튼:

- `확인`

주의:

- “세션 연장” 버튼을 두지 않습니다.
- absolute timeout은 정책상 연장 불가입니다.

---

## 7. 프론트 수용 기준

아래가 모두 만족되면 프론트 작업 완료로 판단합니다.

### Idle 흐름

1. 로그인 후 9분 무활동 시 idle warning 모달이 뜬다.
2. 9분 이전에 활동이 있으면 뜨지 않는다.
3. 9분 이후라도 활동이 다시 발생하면 모달이 닫힌다.
4. 10분이 되면 `idle=true` heartbeat가 즉시 전송된다.
5. `idle_rejected=true` 응답을 받으면 즉시 logout 된다.
6. 다른 탭 활동이 broadcast되면 idle warning도 닫힌다.

### Absolute 흐름

1. 절대 만료 5분 이내면 absolute warning 모달이 뜬다.
2. 회전 응답에서 `absolute_expires_at`를 받아도 countdown 기준은 유지된다.
3. `absolute_expired=true` 또는 `401`이면 즉시 logout 된다.

### 통합

1. 페이지 새로고침 후에도 `/auth/session` 응답 기반으로 warning 상태가 재구성된다.
2. logout 시 warning 상태가 모두 초기화된다.
3. idle warning과 absolute warning이 충돌하면 absolute warning이 우선한다.

---

## 8. 테스트 권장안

프론트팀이 바로 구현할 수 있도록 최소 테스트 케이스도 같이 남깁니다.

### 단위 테스트

- `useSessionWarnings`
  - idle warning open
  - idle activity reset
  - absolute warning open
  - absolute warning does not close on user activity

### 훅/상호작용 테스트

- `useHeartbeat`
  - rotated response updates `absolute_expires_at`
  - `idle_rejected` triggers logout

### UI 테스트

- `SessionIdleWarningModal`
  - extend 클릭 시 닫힘 + heartbeat(false)
  - logout 클릭 시 logout

- `SessionAbsoluteWarningModal`
  - 카운트다운 표시
  - 확인 후 유지/닫기 정책 확인

---

## 9. 구현 시 함정

1. `absolute_expires_at`는 **seconds** 입니다.
   - JS `Date.now()`는 ms이므로 변환해야 합니다.

2. idle warning은 로컬 activity 기준이고,
   absolute warning은 서버 기준 절대 시각 기준입니다.
   - 둘을 같은 타이머로 처리하지 마세요.

3. idle warning에서 “연장” 버튼은 `heartbeat(false)`로 충분하지만,
   버튼 클릭만으로 끝내지 말고 local activity state도 같이 reset 해야 합니다.

4. `idle_rejected=true`는 “경고”가 아니라 **즉시 종료 신호**입니다.

5. `absolute_warning_window_ms`는 경고 창일 뿐 만료 연장 수단이 아닙니다.

---

## 10. 백엔드 구현 위치

- `backend/internal/config/config.go` — `SessionConfig`: heartbeat/idle/absolute/rotation 정책과 검증
- `backend/internal/app/middleware.go` — auth / CSRF 미들웨어
- `backend/internal/app/session_handlers.go` — login / logout / heartbeat / session 핸들러
- `backend/internal/session/session.go`, `backend/internal/session/lifecycle.go` — Valkey 세션 store, Lua CAS 기반 refresh/rotate
- `backend/internal/openapi/spec.json` → `backend/docs/swagger.json` — OpenAPI SSOT와 미러

---

## 11. 백엔드 검증 근거

핸들러 계약 (`backend/internal/app/app_test.go`):

- `go test ./internal/app/ -run TestSessionStatusAuthenticated` — `GET /auth/session`가 `authenticated`/`username`/`session_policy`를 반환
- `go test ./internal/app/ -run TestHeartbeatResultContract` — heartbeat의 refreshed / rotated / idle / absolute-expired / missing 계약 (`absolute_expires_at`, `csrf_token`, `idle_rejected`, `absolute_expired`)
- `go test ./internal/app/ -run TestPlainHeartbeatReissuesSessionCookie` — 회전이 없는 일반 heartbeat도 세션 쿠키를 재발급하고 `absolute_expires_at`를 반환하며 `csrf_token`은 내리지 않음
- `go test ./internal/app/ -run TestRotatedSessionOnlyAllowsHeartbeat` — 회전된 구 세션은 heartbeat만 허용하고 새 CSRF 토큰을 발급
- `go test ./internal/app/ -run TestHeartbeatInvalidPayload` — malformed heartbeat body는 HTTP 400
- `go test ./internal/app/ -run TestLoginSuccessSetsCookies` — 로그인 성공 시 session/CSRF 쿠키 설정

세션 store 동작 (`backend/internal/session/store_integration_test.go`, Valkey 컨테이너 필요):

- `TestStoreRefreshExtendsSession`, `TestStoreRefreshIdleShortens` — refresh 연장 / idle TTL 단축
- `TestStoreRotateBeforeIntervalIsNoop`, `TestStoreRotateAfterIntervalCreatesReplacement` — rotation interval 경계
- `TestStoreGetDropsAbsolutelyExpired` — 절대 만료 세션 제거

세션 정책 검증 (`backend/internal/config/config_test.go`, `config_more_test.go`):

- `TestSessionConfigValidation`, `TestSessionConfigValidateFailureBranches` — `rotation_interval < expiry_duration` 등 세션 정책 제약
- `TestForwardedTrustWarning`, `TestHB04ParseTrustedProxyCIDRs_e8fc8b7d` — forwarded-header 신뢰 경고 / `TRUSTED_PROXY_CIDRS` 파싱

이 문서 기준으로 프론트팀은 추가 백엔드 변경 없이 바로 구현을 시작할 수 있습니다.

---

## 12. 프론트 구현 작업 분해

아래는 프론트 구현자가 그대로 따라갈 수 있는 **실행 순서 기준 작업 분해**입니다.

원칙:

- 한 작업은 한 번에 하나의 책임만 가진다.
- 선행 작업이 끝나야 다음 작업으로 넘어간다.
- 각 작업은 변경 파일, 완료 조건, 테스트 조건을 가진다.
- 아래 순서대로 구현하면 중간 상태에서도 앱이 망가지지 않는다.

### Phase 0. 준비

#### Task 0-1. 현재 세션 bootstrap 데이터 흐름 확인

목표:

- 현재 `authApi.getSession()` 결과가 어디서 소비되는지 확인하고 변경 지점을 고정한다.

읽을 파일:

- `frontend/src/api/core.ts`
- `frontend/src/hooks/useAuthBootstrap.ts`
- `frontend/src/stores/authStore.ts`
- `frontend/src/App.tsx`

산출물:

- `absolute_expires_at`와 `session_policy`를 어디 store에 넣을지 결정

완료 조건:

- session bootstrap 경로를 다이어그램 없이 설명 가능해야 한다.

#### Task 0-2. 파일 생성/수정 범위 고정

필수 수정:

- `frontend/src/hooks/useAuthBootstrap.ts`
- `frontend/src/stores/authStore.ts`
- `frontend/src/hooks/useActivityDetection.ts`
- `frontend/src/hooks/useHeartbeat.ts`
- `frontend/src/App.tsx`

신규 생성:

- `frontend/src/stores/sessionWarningStore.ts`
- `frontend/src/hooks/useSessionWarnings.ts`
- `frontend/src/components/auth/SessionIdleWarningModal.tsx`
- `frontend/src/components/auth/SessionAbsoluteWarningModal.tsx`

선택 생성:

- `frontend/src/lib/sessionWarningTime.ts`

완료 조건:

- 구현자가 어떤 파일에 무엇을 넣을지 이미 결정된 상태여야 한다.

---

### Phase 1. 데이터/상태 기반 만들기

#### Task 1-1. `sessionWarningStore.ts` 생성

목표:

- pre-warning 관련 상태를 인증 상태와 분리된 store로 만든다.

권장 state:

```ts
type SessionWarningState = {
  absoluteExpiresAt: number | null
  policy: SessionPolicy | null
  lastActivityAtMs: number
  idleWarningOpen: boolean
  absoluteWarningOpen: boolean
}
```

필수 액션:

```ts
setSessionPolicy(policy)
setAbsoluteExpiresAt(unixSeconds)
markSessionActivity(nowMs)
openIdleWarning()
closeIdleWarning()
openAbsoluteWarning()
closeAbsoluteWarning()
resetSessionWarnings()
```

변경 파일:

- 신규 `frontend/src/stores/sessionWarningStore.ts`

완료 조건:

- 다른 훅/컴포넌트가 warning 상태를 이 store만 보고 읽고 쓸 수 있어야 한다.

테스트 포인트:

- store 초기화
- policy 저장
- activity timestamp 갱신
- modal open/close
- reset

#### Task 1-2. `authStore.ts`와 역할 경계 정리

목표:

- 인증 여부는 `authStore`, warning 상태는 `sessionWarningStore`가 담당하도록 경계를 명확히 한다.

해야 할 일:

- `authStore.logout()`를 호출하는 곳에서 warning reset도 빠지지 않게 설계
- warning 관련 필드를 `authStore`에 우겨 넣지 않기

변경 파일:

- `frontend/src/stores/authStore.ts`
- `frontend/src/stores/sessionWarningStore.ts`

완료 조건:

- 인증 store와 warning store 책임이 섞이지 않는다.

---

### Phase 2. bootstrap 시 서버 정책 주입

#### Task 2-1. `useAuthBootstrap.ts`에서 session policy 저장

목표:

- 로그인/새로고침 직후 서버 정책과 절대 만료 시각을 store에 넣는다.

해야 할 일:

1. `authApi.getSession()` 성공 시
   - `setAuthenticated(session.authenticated)`
   - `setSessionPolicy(session.session_policy)`
   - `setAbsoluteExpiresAt(session.absolute_expires_at)`
   - `markSessionActivity(Date.now())`
2. 실패 시
   - `setAuthenticated(false)`
   - `resetSessionWarnings()`

변경 파일:

- `frontend/src/hooks/useAuthBootstrap.ts`

완료 조건:

- 새로고침 직후에도 warning 계산에 필요한 데이터가 store에 들어간다.

테스트 포인트:

- 성공 bootstrap
- 실패 bootstrap
- logout/unauthorized fallback 시 reset

#### Task 2-2. `core.ts` 타입 사용 지점 정리

목표:

- session response 확장 필드를 소비하는 코드가 타입 안전하게 동작하게 한다.

변경 파일:

- `frontend/src/api/core.ts`
- `frontend/src/hooks/useAuthBootstrap.ts`

완료 조건:

- `absolute_expires_at`, `session_policy.*`에 `any` 없이 접근한다.

---

### Phase 3. activity와 warning 계산 연결

#### Task 3-1. `useActivityDetection.ts`에서 activity timestamp 연동

목표:

- 모든 사용자 활동이 warning 계산용 `lastActivityAtMs`에 반영되게 한다.

해야 할 일:

- 기존 `resetTimer()` 또는 `resetTimerInternal()` 흐름에서
  - local activity
  - broadcast 수신 activity
  양쪽 모두 `markSessionActivity(Date.now())` 또는 동등한 타임스탬프 갱신 수행

주의:

- broadcast 수신 시 무한 핑퐁이 생기지 않도록 기존 분리 구조는 유지

변경 파일:

- `frontend/src/hooks/useActivityDetection.ts`

완료 조건:

- 같은 탭 활동과 다른 탭 활동 모두 idle warning 닫힘 조건을 만족한다.

테스트 포인트:

- local activity updates timestamp
- broadcast activity updates timestamp
- throttle 유지

#### Task 3-2. `useSessionWarnings.ts` 생성

목표:

- idle/absolute warning을 계산하는 전용 훅을 만든다.

입력:

- `isAuthenticated`
- `isIdle`
- `policy`
- `absoluteExpiresAt`
- `lastActivityAtMs`

해야 할 일:

1. idle warning open 조건 계산
2. idle warning close 조건 계산
3. absolute warning open 조건 계산
4. absolute warning close/reset 조건 계산
5. logout 시 cleanup

권장 구현:

- `setTimeout` 기반 예약이 가장 효율적
- 단순 구현이 필요하면 `setInterval(1000)`도 가능

우선순위 규칙:

- absolute warning이 열리면 absolute warning을 우선 표시
- idle warning은 activity가 생기면 닫기
- absolute warning은 activity로 닫지 않기

변경 파일:

- 신규 `frontend/src/hooks/useSessionWarnings.ts`

완료 조건:

- hook만 연결하면 모달 open/close 상태가 store에 자동 반영된다.

테스트 포인트:

- idle warning open at warning threshold
- idle warning closes on activity
- absolute warning opens at absolute threshold
- absolute warning does not close on activity

---

### Phase 4. heartbeat와 절대 만료 동기화

#### Task 4-1. `useHeartbeat.ts`에서 rotated absolute expiry 갱신

목표:

- 회전 응답으로 들어온 `absolute_expires_at`를 store에 반영한다.

해야 할 일:

- `response.absolute_expires_at`가 있으면 `setAbsoluteExpiresAt(...)`

이유:

- bootstrap 이후 장시간 사용 시에도 absolute warning countdown 기준이 store와 계속 일치해야 함

변경 파일:

- `frontend/src/hooks/useHeartbeat.ts`

완료 조건:

- 회전 응답 후 absolute warning 기준 시각이 최신 상태를 유지한다.

테스트 포인트:

- rotated response updates store

#### Task 4-2. idle warning의 “세션 연장” 액션 연결점 준비

목표:

- 모달 버튼에서 재사용할 수 있는 `heartbeat(false)` 연장 경로를 노출하거나 재사용한다.

선택지:

1. `useHeartbeat.ts` 내부 로직 일부를 공용 helper로 분리
2. 모달에서 직접 `authApi.heartbeat(false)` 호출

권장:

- 현재 구조를 크게 흔들지 않으려면 작은 공용 helper 또는 action 함수 노출

완료 조건:

- idle warning modal의 “세션 연장” 버튼이 서버 연장을 호출할 수 있다.

---

### Phase 5. UI 구현

#### Task 5-1. `SessionIdleWarningModal.tsx` 생성

목표:

- idle warning 표시/연장/로그아웃 UI를 구현한다.

표시 조건:

- `idleWarningOpen === true`
- 가능하면 `absoluteWarningOpen === false`

UI 요구:

- 제목
- 본문
- 남은 시간 텍스트
- `세션 연장` 버튼
- `로그아웃` 버튼

권장 카피:

- 제목: `곧 자동 로그아웃됩니다`
- 본문: `활동이 없어 잠시 후 로그아웃됩니다. 계속 사용하려면 연장하세요.`
- 보조 문구: `약 1분 뒤 자동 로그아웃됩니다.`

버튼 동작:

##### 세션 연장

1. `markSessionActivity(Date.now())`
2. `closeIdleWarning()`
3. `heartbeat(false)` 또는 동등한 연장 호출

##### 로그아웃

1. `authApi.logout()` best-effort
2. `logout()`
3. `resetSessionWarnings()`

변경 파일:

- 신규 `frontend/src/components/auth/SessionIdleWarningModal.tsx`

완료 조건:

- 조건이 맞을 때만 뜨고, 연장/로그아웃 액션이 정상 동작한다.

#### Task 5-2. `SessionAbsoluteWarningModal.tsx` 생성

목표:

- 절대 만료 경고 UI를 구현한다.

표시 조건:

- `absoluteWarningOpen === true`

UI 요구:

- 제목
- 본문
- 남은 시간 카운트다운
- `확인` 버튼

권장 카피:

- 제목: `세션이 곧 만료됩니다`
- 본문: `보안을 위해 세션이 곧 만료됩니다. 진행 중인 작업을 저장해주세요.`
- 보조 문구: `절대 만료 시간은 연장되지 않습니다.`

주의:

- “세션 연장” 버튼 넣지 않기
- activity로 닫히지 않게 하기

변경 파일:

- 신규 `frontend/src/components/auth/SessionAbsoluteWarningModal.tsx`

완료 조건:

- 절대 만료 창에서 안정적으로 표시되고, idle warning보다 우선한다.

---

### Phase 6. 앱 조립

#### Task 6-1. `App.tsx`에 warning hook 연결

목표:

- 기존 인증/heartbeat 흐름에 warning 계산 훅을 연결한다.

해야 할 일:

- `ProtectedRoute` 안에서 `useSessionWarnings(...)` 호출

변경 파일:

- `frontend/src/App.tsx`

완료 조건:

- 로그인 상태에서만 warning 로직이 살아있다.

#### Task 6-2. 전역 modal 렌더링 추가

목표:

- 모달을 앱 전역에서 띄울 수 있게 한다.

해야 할 일:

- `ProtectedRoute` 또는 레이아웃 상단에
  - `<SessionIdleWarningModal />`
  - `<SessionAbsoluteWarningModal />`
  배치

주의:

- route transition과 무관하게 유지되는 위치에 두기
- absolute warning 우선 렌더 규칙 보장

완료 조건:

- 어느 dashboard 화면에 있어도 warning이 정상 표시된다.

---

### Phase 7. 검증

#### Task 7-1. 단위 테스트 추가

필수:

- `sessionWarningStore`
- `useSessionWarnings`
- idle threshold
- absolute threshold
- activity reset

#### Task 7-2. heartbeat 회귀 테스트 보강

필수:

- rotated response updates `absolute_expires_at`
- idle_rejected keeps immediate logout

#### Task 7-3. UI 상호작용 테스트

필수:

- idle modal open
- idle modal extend
- idle modal logout
- absolute modal open
- absolute modal countdown text

#### Task 7-4. 수동 QA

시나리오:

1. 로그인
2. 9분 대기 → idle modal 표시
3. 연장 버튼 클릭 → modal 닫힘 / 세션 유지
4. 다시 10분 대기 → idle=true heartbeat / 즉시 logout
5. 절대 만료 기준 5분 이하 mock → absolute modal 표시
6. 401 absolute_expired → 즉시 logout

---

## 13. 작업 순서 요약

프론트 구현자는 아래 순서대로 하면 됩니다.

1. `sessionWarningStore.ts` 생성
2. `useAuthBootstrap.ts`에 policy / absolute expiry 저장
3. `useActivityDetection.ts`에 activity timestamp 연동
4. `useSessionWarnings.ts` 생성
5. `useHeartbeat.ts`에 rotated absolute expiry store 갱신 추가
6. `SessionIdleWarningModal.tsx` 생성
7. `SessionAbsoluteWarningModal.tsx` 생성
8. `App.tsx`에 hook + modal 조립
9. 테스트 추가
10. 수동 QA

이 순서를 지키면 의존성 꼬임 없이 구현할 수 있습니다.
