# Session Auth 앱 구현 상세 명세서

> 마지막 업데이트: 2026-01-04  
> 대상 서버: `hololive-kakao-bot-go`  
> API Prefix: `/api/auth`

본 문서는 앱(Tauri/모바일/웹뷰 등)이 **세션 기반 인증(Bearer Token)** 을 구현할 때 필요한 상세 스펙을 제공합니다.

---

## 기본 정보

| 항목 | 값 |
|---|---|
| Base URL (dev) | `http://localhost:30001` |
| Base URL (prod) | `https://api.capu.blog` (환경에 맞게 변경) |
| 인증 방식 | `Authorization: Bearer <token>` |
| 토큰 저장 | 클라이언트 보안 저장소(권장) |
| 서버 세션 | Stateful (Valkey/Redis 기반) |

---

## 0. 중요한 전제

1. 이 인증은 쿠키가 아니라 **Authorization 헤더** 기반입니다.
2. 서버는 세션을 Valkey/Redis에 저장하고 검증합니다. 따라서:
   - 클라이언트가 가진 토큰이 `expiresAt` 이전이라도 서버에서 폐기되면 즉시 무효가 될 수 있습니다.
   - 비밀번호 변경 시 기존 세션이 전부 폐기됩니다(로그아웃 처리 필요).
3. 현재 서버의 `/api/holo/*` 는 `X-API-Key` 기반입니다. 앱이 `/api/holo/*` 를 직접 호출해야 한다면 **API Key를 앱에 포함하는 구조는 보안상 위험**합니다. (권장: `/api/holo/*` 를 세션 기반으로 전환하거나, 서버 내부 프록시를 둠)

---

## 1. 클라이언트 저장 모델

### 1.1 권장 저장 형태

```ts
export interface Session {
  token: string;      // "sess_..." (opaque, secret)
  expiresAt: string;  // RFC3339 (UTC)
}

export interface User {
  id: string;               // uuid string
  email: string;
  displayName: string;
  avatarUrl: string | null;
  createdAt?: string;       // /me, /register에서 제공
}

export interface AuthState {
  session: Session;
  user: User;
}
```

### 1.2 저장소/보안 권장

- 토큰은 **보안 저장소**(Keychain/Keystore 또는 tauri-plugin-store 암호화) 사용 권장
- `Authorization` 헤더/토큰을 로그/크래시 리포트에 남기지 말 것
- 토큰은 절대 URL/querystring에 넣지 말 것

### 1.3 저장소 성능 최적화 (권장 패턴)

OS의 보안 저장소(Keychain/Keystore) 접근은 디스크 I/O 및 암호화 연산을 수반하여 수~수십 ms가 소요됩니다.
매 API 요청마다 보안 저장소를 호출하면 성능 저하가 발생할 수 있습니다.

**권장 패턴:**
- **Write-Through**: 로그인/갱신 시점에 보안 저장소에 저장
- **Read-Cache**: 앱 시작(Splash) 시점에 1회 읽어 **메모리**(Rust State 또는 JS Store)에 캐싱, API 호출 시에는 메모리 값 사용

---

## 2. 공통 HTTP 규격

### 2.1 공통 헤더

- JSON 요청:
  - `Content-Type: application/json`
  - `Accept: application/json`
- 인증 필요 요청:
  - `Authorization: Bearer <session.token>`

### 2.2 시간 포맷

- 서버가 내려주는 시간 문자열은 **RFC3339(UTC)** 입니다.
  - 예: `2026-01-04T10:00:00Z`

### 2.3 공통 응답 형식

#### 성공
```json
{
  "success": true
}
```

#### 실패
```json
{
  "success": false,
  "error": "ERROR_CODE"
}
```

### 2.4 에러 코드/상태코드 매핑(클라이언트 처리 기준)

| HTTP | error | 의미 | 권장 UX/처리 |
|---:|---|---|---|
| 400 | `INVALID_INPUT` | 입력값 검증 실패 | 입력 폼 에러 표시 |
| 401 | `INVALID_CREDENTIALS` | 이메일/비밀번호 불일치 | “이메일/비밀번호 확인” |
| 401 | `UNAUTHORIZED` | 세션 무효/만료/누락 | 로컬 세션 삭제 후 로그인 유도 |
| 403 | `ACCOUNT_LOCKED` | 계정 잠금(과다 시도) | 잠금 안내 + 재시도 제한 |
| 409 | `EMAIL_EXISTS` | 이미 가입된 이메일 | “이미 가입된 이메일” |
| 429 | `RATE_LIMITED` | 요청 제한 초과 | 백오프 + 안내 |
| 500 | `INTERNAL_ERROR` | 서버 내부 오류 | 일시 장애 안내 |

> **향후 개선 예정**: `UNAUTHORIZED`를 `TOKEN_EXPIRED`(갱신 시도 가능)와 `SESSION_INVALID`(즉시 로그인 필요)로 세분화하여 클라이언트가 불필요한 `/refresh` 시도 없이 적절한 동작을 수행할 수 있도록 할 예정입니다.

---

## 3. 엔드포인트 상세 명세

## 3.1 회원가입

### `POST /api/auth/register`

**Request**
```json
{
  "email": "user@example.com",
  "password": "Password1",
  "displayName": "User"
}
```

**Response (201)**
```json
{
  "success": true,
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "displayName": "User",
    "createdAt": "2026-01-04T10:00:00Z"
  }
}
```

**Validation (서버 기준)**
- `email`: RFC 형식 파싱 가능해야 함(서버에서 lowercase+trim 처리)
- `password`: 길이 8~72, 영문 1개 이상 + 숫자 1개 이상
- `displayName`: trim 후 non-empty, 64 rune 이하

---

## 3.2 로그인

### `POST /api/auth/login`

**Request**
```json
{
  "email": "user@example.com",
  "password": "Password1"
}
```

**Response (200)**
```json
{
  "success": true,
  "session": {
    "token": "sess_xxxxxxxxxxxxxxxxxxxx",
    "expiresAt": "2026-01-11T10:00:00Z"
  },
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "displayName": "User",
    "avatarUrl": null
  }
}
```

**서버 정책(현재 기본값)**
- 로그인 실패 누적: 15분 윈도우 내 5회 실패 시 15분 잠금(403 `ACCOUNT_LOCKED`)
- 로그인 레이트리밋: IP 기준 분당 30회 초과 시 429 `RATE_LIMITED`

> **CGNAT 환경 주의**: 모바일 네트워크(LTE/5G)는 CGNAT(Carrier-Grade NAT)를 사용하여 다수의 사용자가 동일한 공인 IP를 공유합니다. IP 기반 Rate Limit은 무고한 사용자에게 영향을 줄 수 있으므로, 계정 기반 잠금(`ACCOUNT_LOCKED`)이 주요 Brute Force 방어 메커니즘입니다.

---

## 3.3 로그아웃

### `POST /api/auth/logout`

**Headers**
```http
Authorization: Bearer sess_xxx
```

**Response (200)**
```json
{
  "success": true
}
```

**클라이언트 처리**
- 성공 시 로컬 저장소의 `AuthState` 삭제

---

## 3.4 세션 갱신

### `POST /api/auth/refresh`

**Headers**
```http
Authorization: Bearer sess_xxx
```

**Response (200)**
```json
{
  "success": true,
  "session": {
    "token": "sess_yyyyyyyyyyyyyyyyyyyy",
    "expiresAt": "2026-01-18T10:00:00Z"
  }
}
```

**중요**
- refresh 성공 시 **기존 토큰은 즉시 무효화**됩니다.
- 클라이언트는 토큰 저장을 “원자적으로” 수행해야 합니다:
  1) refresh 성공
  2) 새 토큰 저장(쓰기 성공 보장)
  3) 이후 요청부터 새 토큰 사용

**Grace Period (유예 기간)**
- 서버는 토큰 교체 후 **30초 간** 기존 토큰도 유효하게 처리합니다.
- 이는 동시에 여러 요청이 진행 중일 때, "In-flight" 상태의 구 토큰 요청이 실패하지 않도록 보호합니다.
- 클라이언트 Single-flight 구현과 함께 사용하면 동시성 문제를 완전히 해소할 수 있습니다.

---

## 3.5 현재 사용자 조회

### `GET /api/auth/me`

**Headers**
```http
Authorization: Bearer sess_xxx
```

**Response (200)**
```json
{
  "success": true,
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "displayName": "User",
    "avatarUrl": null,
    "createdAt": "2026-01-04T10:00:00Z"
  }
}
```

---

## 3.6 비밀번호 재설정

### `POST /api/auth/password/reset-request`

**Request**
```json
{
  "email": "user@example.com"
}
```

**Response (200)**
```json
{
  "success": true,
  "message": "If the email exists, a reset link has been sent."
}
```

**주의**
- 현재 서버는 토큰 생성/저장까지만 수행하며, 이메일 발송은 별도 연동이 필요합니다.

---

### `POST /api/auth/password/reset`

**Request**
```json
{
  "token": "reset_token_from_email",
  "newPassword": "NewPassw0rd1"
}
```

**Response (200)**
```json
{
  "success": true
}
```

**토큰 정책**
- 유효 시간: **15분** (생성 시점 기준)
- 사용 횟수: **1회용** (사용 즉시 폐기)
- 만료 시 안내: "링크가 만료되었습니다. 다시 요청해 주세요."

**보안 동작**
- 비밀번호 변경 성공 시 해당 유저의 기존 세션이 전부 폐기됩니다.
- 클라이언트는 이후 요청에서 401 `UNAUTHORIZED` 를 받을 수 있으므로, 상태를 정리하고 로그인 화면으로 유도해야 합니다.

---

## 4. 권장 클라이언트 플로우

## 4.1 앱 시작 시

1) 저장소에서 `AuthState` 로드  
2) 없으면 비로그인  
3) 있으면 `/api/auth/me` 로 검증  
   - 200: 로그인 상태 유지(유저 정보 갱신)  
   - 401: 저장소 삭제 후 로그인 화면  

## 4.2 요청 시 토큰 부착 및 자동 복구

- 모든 인증 요청에 `Authorization: Bearer <token>` 자동 부착
- 응답이 401 `UNAUTHORIZED` 인 경우:
  - refresh를 **최대 1회** 시도
  - refresh 성공 시 원 요청 1회 재시도
  - 재시도 실패 또는 refresh 실패 시 로컬 세션 삭제 후 로그인 유도

## 4.3 동시성(필수)

동시에 여러 요청이 401을 받을 수 있으므로, **refresh single-flight** 를 구현해야 합니다.

권장 정책:
- refresh 진행 중이면 다른 요청은 refresh 결과를 기다렸다가 재시도/로그아웃 처리
- refresh는 “중복 실행 금지”

## 4.4 refresh 타이밍(권장)

- `expiresAt` 기준 만료 임박 시 사전 refresh 권장(예: 만료 24시간 전 1회)
- 완전 만료 후에는 refresh가 실패할 수 있으므로 “만료 전 갱신”이 안전합니다.

---

## 5. 개발 환경 가이드

### 5.1 Android 에뮬레이터 환경

명세서의 Base URL이 `localhost:30001`로 되어 있으나, Android 에뮬레이터에서 `localhost`는 **에뮬레이터 자신**을 가리킵니다.

**해결 방법:**
- 표준 Android 에뮬레이터: `10.0.2.2:30001` 사용 (호스트 머신의 loopback)
- Genymotion: `10.0.3.2:30001`
- 실기기/로컬 네트워크: `192.168.x.x:30001` (개발 머신의 LAN IP)

```typescript
// 예시: 플랫폼별 Base URL 분기
const getBaseUrl = () => {
  if (import.meta.env.DEV) {
    // Android 에뮬레이터 감지 시
    if (window.__TAURI__ && navigator.userAgent.includes('Android')) {
      return 'http://10.0.2.2:30001'\;
    }
    return 'http://localhost:30001'\;
  }
  return 'https://api.capu.blog'\;
};
```
