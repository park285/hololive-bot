# Auth Core 단일화 초안

> 작성일: 2026-03-06  
> 범위:  
> - `hololive/hololive-kakao-bot-go/internal/service/auth/service.go`  
> - `hololive/hololive-shared/pkg/service/auth/service.go`

---

## 목적

현재 bot/shared auth service는 구현이 사실상 동일하여, 세션/레이트리밋/비밀번호 재설정 로직이 이중 유지보수 상태입니다.  
이번 단계에서는 즉시 통합하지 않고, **안전하게 공통 core를 추출하는 범위**를 정의합니다.

---

## 현황

- 텍스트/로직 중복도가 매우 높음
- 이번 리팩토링에서 `createSession()`은 양쪽 모두 `SetNX` 기반으로 동일하게 원자화됨
- 그러나 아래 레이어는 아직 패키지 경로에 묶여 별도 유지되고 있음
  - config
  - error code
  - 모델 변환
  - DB schema bootstrap

---

## 제안 구조

### 1. shared auth core 패키지 도입

후보:
- `hololive/hololive-shared/pkg/service/authcore`

역할:
- `Service` 공통 비즈니스 로직 보유
- 세션 발급/검증/refresh/logout
- rate limit / login fail / account lock
- password reset token 발급/검증

### 2. consumer별 얇은 facade 유지

- `hololive-kakao-bot-go/internal/service/auth`
- `hololive-shared/pkg/service/auth`

facade 책임:
- 현재 외부 공개 생성자 시그니처 유지
- consumer별 wiring만 담당
- 필요 시 consumer 로깅/오류 매핑만 얇게 유지

---

## 분리 기준

### 공통 core로 이동할 항목

- `Login`
- `Logout`
- `Refresh`
- `Me`
- `RequestPasswordReset`
- `ResetPassword`
- `createSession`
- `validateSession`
- `revokeAllSessions`
- login fail / lock / rate limit helper
- token/hash helper

### facade에 남길 항목

- `NewService(...)`
- schema bootstrap 여부 결정
- consumer별 package path/export 정리

---

## 인터페이스 초안

```go
type UserStore interface {
	CreateUser(ctx context.Context, user *UserRecord) error
	FindUserByEmail(ctx context.Context, email string) (*UserRecord, error)
	FindUserByID(ctx context.Context, id string) (*UserRecord, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
}

type ResetTokenStore interface {
	Save(ctx context.Context, token ResetTokenRecord) error
	FindValid(ctx context.Context, tokenHash string, now time.Time) (*ResetTokenRecord, error)
	MarkUsed(ctx context.Context, tokenHash string, usedAt time.Time) error
}

type SessionCache interface {
	Get(ctx context.Context, key string, dest any) error
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	SAdd(ctx context.Context, key string, members []string) (int64, error)
	SRem(ctx context.Context, key string, members []string) (int64, error)
	SMembers(ctx context.Context, key string) ([]string, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
	DelMany(ctx context.Context, keys []string) (int64, error)
	Exists(ctx context.Context, key string) (bool, error)
}
```

---

## 단계적 이전 순서

1. **Phase A**
   - token/hash/session helper를 `authcore`로 이동
   - 기존 public API 불변

2. **Phase B**
   - session / refresh / revokeAllSessions 이동
   - bot/shared 양쪽 facade thin wrapper화

3. **Phase C**
   - user store / reset token store interface 분리
   - schema bootstrap 분리

4. **Phase D**
   - 중복 테스트 제거
   - 공통 contract test 추가

---

## 리스크

- facade와 core 사이 error code 매핑이 어긋나면 API 동작이 바뀔 수 있음
- DB bootstrap까지 한 번에 옮기면 diff가 커짐
- auth는 보안 민감 영역이므로 단계적 이전이 안전함

---

## 완료 기준

- bot/shared auth service가 동일 core를 호출
- 세션/refresh/reset contract test 공통화
- 기존 public 생성자/호출부 변경 최소화
