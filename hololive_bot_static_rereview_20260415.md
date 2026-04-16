# Hololive Bot Bundle 재리뷰 보고서 (2026-04-15)

## 0. 이번 재리뷰의 결론

이번 번들은 **이전 리뷰에서 가장 위험하다고 본 몇 가지 핵심 버그를 실제로 고친 흔적이 분명히 보인다.**
특히 아래 항목은 회귀 없이 정리된 것으로 판단된다.

- `youtube/outbox` claim ownership 버그 수정
  - `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim_gate.go`
  - `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go`
  - `rowClaimTokens [][]deliveryClaimToken` 기반으로 행별 ownership이 분리되었다.
- `cache.Service.MSet` marshal fail-fast 수정
  - `hololive/hololive-shared/pkg/service/cache/service.go`
- `Holodex SearchChannels`가 전체 채널 캐시 경로를 타도록 수정
  - `hololive/hololive-shared/pkg/service/holodex/service_channels.go`
- Holodex 기본 HTTP client가 external API transport를 사용하도록 수정
  - `hololive/hololive-shared/pkg/service/holodex/api_client.go`
- `ACL` 서비스가 롤백 의미를 가지도록 재구성됨
  - `hololive/hololive-kakao-bot-go/internal/service/acl/service_mutation.go`
  - 관련 rollback 테스트도 같이 보강됨
- `Chzzk` logger nil 기본값 문제는 해결됨
  - `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go`
- `admin-dashboard`의 세션 refresh payload `expires_at` 갱신 문제도 해결된 상태로 보임
  - `admin-dashboard/backend/src/auth/session.rs`

즉, 이번 번들은 “이전 지적을 무시한 번들”이 아니라, **상당 부분 반영된 번들**이다.

다만 남아 있는 문제는 여전히 있다. 이번 번들의 핵심 리스크는 아래 네 축이다.

1. **취소/타임아웃 경계가 잘못되어 DB 작업이 요청 생명주기를 벗어나 오래 살아남는 문제**
2. **부하 시 worker pool backpressure를 무력화하는 비동기 실행 경로**
3. **세션/보안 상태의 정합성을 cache 인덱스 실패에 맡기는 인증 서비스**
4. **비결정적 순서, 얇은 wrapper, 큰 repository 파일 등 유지보수 부채**

아래는 그 내용을 운영 리스크와 패치 난이도 기준으로 정렬한 상세안이다.

---

## 1. 검토 범위와 한계

전체 번들을 다시 풀어서 Go monorepo와 `admin-dashboard`까지 함께 스캔했다.
정적 분석, 구조 비교, 중복/책임 분리 관점 리뷰, 라인 수 정책 확인까지는 수행했다.

다만 **실컴파일/실테스트는 이번에도 완전하게 수행하지 못했다.** 이유는 두 가지다.

- 번들 추출본이라 `.git` 메타데이터가 없어 일부 구조 검사 스크립트가 완전하게 동작하지 않음
- 저장소가 `go 1.26.2` / `toolchain go1.26.2`를 요구하는데 현재 환경은 해당 toolchain 다운로드가 차단되어 있음

실행 결과:

- `scripts/architecture/check-file-loc.sh` : 통과
- `scripts/architecture/check-go-module-loc.sh` : 통과
- `scripts/architecture/check-shared-go-boundary.sh` : **실패**
  - 원인 1: bundle 추출본이라 git repository 아님
  - 원인 2: `go1.26.2` toolchain 다운로드 불가

따라서 본 문서는 **정적 리뷰 기준**이다. 다만 아래 상위 항목들은 동적 실행 없이도 충분히 결함으로 판단 가능한 종류다.

---

## 2. 이번 번들에서 가장 먼저 남은 문제

### 2.1 `alarm/targets.go`: `context.WithoutCancel`로 취소는 끊고 deadline까지 같이 잃어버린다

대상:
- `hololive/hololive-shared/pkg/service/alarm/targets.go:106-156`

현재 코드의 핵심은 다음과 같다.

```go
resultCh := channelSubscriberLoadGroup.DoChan(normalizedChannelID, func() (any, error) {
    queryCtx := context.Background()
    if ctx != nil {
        queryCtx = context.WithoutCancel(ctx)
    }

    var records []domain.Alarm
    if err := db.WithContext(queryCtx).
        Where("channel_id = ?", normalizedChannelID).
        Order("created_at ASC").
        Find(&records).Error; err != nil {
        return nil, fmt.Errorf("load channel subscriber alarms: %w", err)
    }
    ...
})
```

표면 의도는 이해된다. `singleflight` 공유 쿼리의 리더가 중간에 cancel되어도 follower가 계속 결과를 재사용하도록 하려는 것이다.

문제는 `context.WithoutCancel(ctx)`가 **cancel만 제거하는 것이 아니라 parent deadline도 전파하지 않는다**는 점이다.
그 결과 이 DB 쿼리는 다음 상태가 된다.

- 호출자는 timeout이 끝나서 이미 떠났는데
- shared query는 background처럼 계속 살아 있고
- DB가 느리거나 lock이 걸리면 생각보다 오래 붙잡힐 수 있다

이건 성능 문제가 아니라 **자원 회수 실패** 문제다. 요청 폭주 시 가장 먼저 DB connection 체류 시간 증가로 번질 수 있다.

#### 권장 패치

- cancel은 떼되, **deadline은 보존**해야 한다.
- parent deadline이 없으면 fallback timeout을 강제로 부여해야 한다.

아래처럼 고치는 것이 맞다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/alarm/targets.go b/hololive/hololive-shared/pkg/service/alarm/targets.go
@@
 const emptyChannelSubscriberCacheTTL = 30 * time.Second
+const channelSubscriberLoadTimeout = 5 * time.Second
 
 var channelSubscriberLoadGroup singleflight.Group
+
+func withoutCancelPreserveDeadline(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
+    if ctx == nil {
+        return context.WithTimeout(context.Background(), fallback)
+    }
+
+    base := context.WithoutCancel(ctx)
+    if deadline, ok := ctx.Deadline(); ok {
+        return context.WithDeadline(base, deadline)
+    }
+
+    return context.WithTimeout(base, fallback)
+}
@@
 	resultCh := channelSubscriberLoadGroup.DoChan(normalizedChannelID, func() (any, error) {
-		queryCtx := context.Background()
-		if ctx != nil {
-			queryCtx = context.WithoutCancel(ctx)
-		}
+		queryCtx, cancel := withoutCancelPreserveDeadline(ctx, channelSubscriberLoadTimeout)
+		defer cancel()
 
 		var records []domain.Alarm
 		if err := db.WithContext(queryCtx).
 			Where("channel_id = ?", normalizedChannelID).
```

#### 추가 테스트

- parent ctx에 100ms deadline 부여
- DB mock이 300ms sleep 후 응답하도록 구성
- `loadChannelSubscriberAlarms`가 deadline 초과를 반환하는지 확인
- same key singleflight follower가 leader 결과 공유는 유지되는지 확인

#### 우선순위

**상**. 운영 중 느린 DB/lock 상황에서 connection 체류를 길게 만드는 문제라서 바로 손대는 편이 맞다.

---

### 2.2 `bot_message_async.go`: worker pool이 막히면 무제한 goroutine으로 우회한다

대상:
- `hololive/hololive-kakao-bot-go/internal/bot/bot_message_async.go:35-88`

현재 코드는 pool submit 실패 시 이렇게 동작한다.

```go
if b.workerPool != nil {
    submitErr := b.workerPool.Submit(task)
    if submitErr == nil {
        return
    }
    ... warn ...
}

go task()
```

이건 가장 위험한 형태의 fallback이다.

부하가 높아 worker pool이 reject하고 있다는 뜻은 이미 시스템이 **backpressure를 걸어야 할 시점**이라는 뜻이다.
그런데 그 순간 pool을 우회해 `go task()`를 뿌리면 다음 일이 벌어진다.

- pool capacity 제한이 의미를 잃음
- burst traffic 시 goroutine 수가 폭증함
- 메모리 사용량이 급증함
- downstream I/O가 더 막히고, 더 많은 submit failure가 생기고, 다시 goroutine으로 우회함

즉, “부하가 걸릴수록 보호 장치를 해제하는” 구조다.

#### 권장 패치

정책은 둘 중 하나여야 한다.

1. **worker pool이 없을 때만 동기 실행 fallback**
2. **worker pool이 reject하면 명시적으로 throttle/drop**

가장 안전한 패치는 아래다.

- pool 자체가 nil이면 misconfiguration이므로 동기 실행 fallback 허용
- pool이 reject하면 goroutine fallback 금지
- 사용자에게 “잠시 후 다시 시도” 응답을 best-effort로 보냄

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/bot/bot_message_async.go b/hololive/hololive-kakao-bot-go/internal/bot/bot_message_async.go
@@
 	task := func() {
 		defer cancel()
@@
 	}
 
-	if b.workerPool != nil {
-		submitErr := b.workerPool.Submit(task)
-		if submitErr == nil {
-			return
-		}
-
-		if b.logger != nil {
-			b.logger.Warn(
-				"Failed to submit async command task to worker pool; falling back to goroutine",
-				slog.String("command", commandType),
-				slog.Any("error", submitErr),
-			)
-		}
-	}
-
-	go task()
+	if b.workerPool == nil {
+		if b.logger != nil {
+			b.logger.Warn(
+				"Async command worker pool missing; running synchronously",
+				slog.String("command", commandType),
+			)
+		}
+		task()
+		return
+	}
+
+	if submitErr := b.workerPool.Submit(task); submitErr != nil {
+		if b.logger != nil {
+			b.logger.Warn(
+				"Async command rejected by worker pool",
+				slog.String("command", commandType),
+				slog.Any("error", submitErr),
+			)
+		}
+
+		cancel()
+
+		if chatID != "" {
+			notifyCtx, notifyCancel := context.WithTimeout(context.Background(), constants.RequestTimeout.WebhookProcessing)
+			defer notifyCancel()
+			if sendErr := b.sendError(notifyCtx, chatID, "요청이 많아 잠시 후 다시 시도해주세요."); sendErr != nil && b.logger != nil {
+				b.logger.Error("Failed to send async backpressure message", slog.Any("error", sendErr), slog.String("chat_id", chatID))
+			}
+		}
+		return
+	}
 }
```

#### 추가 테스트

- workerPool nil → 동기 실행되는지
- workerPool reject → goroutine fallback 없이 에러 메시지 전송만 시도하는지
- reject 상황에서 goroutine count가 증가하지 않는지 (가능하면 benchmark/pprof 기반)

#### 우선순위

**상**. 이건 단순 코드 스타일이 아니라 **부하 시 보호장치를 풀어버리는 구조**다.

---

### 2.3 `auth/service.go`: 세션 key와 user-session index가 원자적으로 유지되지 않는다

대상:
- `hololive/hololive-shared/pkg/service/auth/service.go:206-259`
- `hololive/hololive-shared/pkg/service/auth/service.go:411-494`
- `hololive/hololive-shared/pkg/service/auth/service.go:563-579`

현재 `createSession()`은 다음 순서다.

1. `SetNX(sessionKey)` 성공
2. `SAdd(userSessionsKey, sessionHash)` **에러 무시**
3. `Expire(userSessionsKey, ttl)` **에러 무시**

```go
acquired, err := s.cacheSvc.SetNX(ctx, k, string(payload), s.cfg.SessionTTL)
...
_, _ = s.cacheSvc.SAdd(ctx, userSessionsKey, []string{sessionHash})
_ = s.cacheSvc.Expire(ctx, userSessionsKey, s.cfg.UserSessionsTTL)
```

이 상태에서 index 갱신이 실패하면 세션은 생성되지만 user-session set에는 들어가지 않는다.
그 결과 `ResetPassword()`의 `revokeAllSessions()`가 이 세션을 못 지울 수 있다.

이건 **보안 결함**이다. 비밀번호를 바꿨는데 일부 세션이 남을 수 있기 때문이다.

또 `Refresh()`도 문제다.

```go
newSession, err := s.createSession(ctx, data.UserID)
...
_ = s.cacheSvc.Del(ctx, oldKey)
_, _ = s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+data.UserID, []string{sessionHash})
```

새 세션 생성 후 기존 세션 무효화 실패를 무시한다. 그러면 rotate가 아니라 **세션 복제**가 된다.

#### 권장 패치 A: `createSession()`은 index 실패 시 세션 key를 rollback

```diff
diff --git a/hololive/hololive-shared/pkg/service/auth/service.go b/hololive/hololive-shared/pkg/service/auth/service.go
@@
 	userSessionsKey := userSessionsKeyPrefix + userID
-	_, _ = s.cacheSvc.SAdd(ctx, userSessionsKey, []string{sessionHash})
-	_ = s.cacheSvc.Expire(ctx, userSessionsKey, s.cfg.UserSessionsTTL)
+	if _, err := s.cacheSvc.SAdd(ctx, userSessionsKey, []string{sessionHash}); err != nil {
+		_ = s.cacheSvc.Del(ctx, sessionKeyPrefix+sessionHash)
+		return nil, newError(CodeInternal, "failed to update session index", err)
+	}
+	if err := s.cacheSvc.Expire(ctx, userSessionsKey, s.cfg.UserSessionsTTL); err != nil {
+		_, _ = s.cacheSvc.SRem(ctx, userSessionsKey, []string{sessionHash})
+		_ = s.cacheSvc.Del(ctx, sessionKeyPrefix+sessionHash)
+		return nil, newError(CodeInternal, "failed to expire session index", err)
+	}
 
 	return &Session{
```

#### 권장 패치 B: refresh 시 old session invalidation 실패를 무시하지 말 것

아래 helper를 추가한다.

```go
func (s *Service) deleteSessionByHash(ctx context.Context, userID, sessionHash string) error {
    if s.cacheSvc == nil {
        return newError(CodeInternal, "cache service not configured", nil)
    }
    key := sessionKeyPrefix + sessionHash
    var errs []error
    if err := s.cacheSvc.Del(ctx, key); err != nil {
        errs = append(errs, fmt.Errorf("delete session key: %w", err))
    }
    if _, err := s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+userID, []string{sessionHash}); err != nil {
        errs = append(errs, fmt.Errorf("remove session index: %w", err))
    }
    return stdErrors.Join(errs...)
}
```

그리고 `Refresh()`를 아래처럼 바꾼다.

```diff
@@
 	newSession, err := s.createSession(ctx, data.UserID)
 	if err != nil {
 		return nil, err
 	}
 
-	// 기존 세션 무효화
-	_ = s.cacheSvc.Del(ctx, oldKey)
-	_, _ = s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+data.UserID, []string{sessionHash})
+	if err := s.deleteSessionByHash(ctx, data.UserID, sessionHash); err != nil {
+		_ = s.deleteSessionByHash(context.Background(), data.UserID, sha256Hex(newSession.Token))
+		return nil, newError(CodeInternal, "failed to invalidate previous session during refresh", err)
+	}
 
 	return newSession, nil
 }
```

#### 권장 패치 C: `revokeAllSessions()`도 실제 에러를 반환

```diff
@@
-	_, _ = s.cacheSvc.DelMany(ctx, keys)
-	_ = s.cacheSvc.Del(ctx, userSessionsKey)
-
-	return nil
+	var errs []error
+	if _, err := s.cacheSvc.DelMany(ctx, keys); err != nil {
+		errs = append(errs, fmt.Errorf("delete session keys: %w", err))
+	}
+	if err := s.cacheSvc.Del(ctx, userSessionsKey); err != nil {
+		errs = append(errs, fmt.Errorf("delete user session index: %w", err))
+	}
+
+	return stdErrors.Join(errs...)
 }
```

#### 권장 패치 D: `incrWithTTL()`은 INCR + EXPIRE를 원자화

현재 구현:

```go
count, err := ... INCR ...
if count == 1 && ttl > 0 {
    _ = cacheSvc.Expire(ctx, key, ttl)
}
```

이건 중간에 프로세스가 죽거나 `Expire`가 실패하면 TTL 없는 key가 남는다.
그 결과 login fail counter나 rate limit counter가 **의도보다 오래 남을 수 있다.**

`auth/service.go`에 Lua script를 추가하는 편이 가장 빠르다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/auth/service.go b/hololive/hololive-shared/pkg/service/auth/service.go
@@
 import (
 	context
 	crypto/rand
 	"crypto/sha256"
 	"encoding/hex"
 	stdErrors "errors"
 	"fmt"
 	"log/slog"
+	"math"
+	"strconv"
 	"strings"
 	"time"
@@
 )
+
+const incrWithTTLScript = `
+local current = redis.call('INCR', KEYS[1])
+if current == 1 and tonumber(ARGV[1]) > 0 then
+  redis.call('EXPIRE', KEYS[1], tonumber(ARGV[1]))
+end
+return current
+`
@@
 func incrWithTTL(ctx context.Context, cacheSvc cache.Client, key string, ttl time.Duration) (int64, error) {
-	results := cacheSvc.DoMulti(ctx, cacheSvc.B().Incr().Key(key).Build())
-	if len(results) != 1 {
-		return 0, fmt.Errorf("increment with ttl: unexpected result count: %d", len(results))
-	}
-	if results[0].Error() != nil {
-		return 0, results[0].Error()
-	}
-	count, err := results[0].AsInt64()
-	if err != nil {
-		return 0, err
-	}
-	// 최초 생성 시에만 TTL 부여
-	if count == 1 && ttl > 0 {
-		_ = cacheSvc.Expire(ctx, key, ttl)
-	}
-	return count, nil
+	ttlSeconds := int64(0)
+	if ttl > 0 {
+		ttlSeconds = int64(math.Ceil(ttl.Seconds()))
+		if ttlSeconds <= 0 {
+			ttlSeconds = 1
+		}
+	}
+
+	cmd := cacheSvc.B().Eval().
+		Script(incrWithTTLScript).
+		Numkeys(1).
+		Key(key).
+		Arg(strconv.FormatInt(ttlSeconds, 10)).
+		Build()
+
+	resp := cacheSvc.GetClient().Do(ctx, cmd)
+	if resp.Error() != nil {
+		return 0, resp.Error()
+	}
+
+	count, err := resp.AsInt64()
+	if err != nil {
+		return 0, err
+	}
+	return count, nil
 }
```

#### 테스트 추가 포인트

- `createSession`: `SAdd` 실패 / `Expire` 실패 시 session key rollback 되는지
- `Refresh`: old session cleanup 실패 시 new session rollback 되는지
- `ResetPassword`: `revokeAllSessions` 실패를 더 이상 삼키지 않는지
- `incrWithTTL`: 첫 증가 시 TTL이 붙고, 이후 증가 시 TTL이 유지되는지

#### 우선순위

**상**. 이건 인증 서비스이므로 “best effort”로 두면 안 된다.

---

### 2.4 `chzzk/client.go`: 생성자가 nil `http.Client`를 허용하고, 네트워크 로직이 과하게 중복되어 있다

대상:
- `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go:73-96`
- `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go:110-212`
- `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go:313-450`

이번 번들에서 logger nil 기본값 문제는 고쳐졌다.
하지만 **http client nil 기본값은 여전히 없다.**

```go
func NewClient(httpClient *http.Client, baseURL string, logger *slog.Logger) *Client {
    return &Client{
        httpClient: httpClient,
        ...
    }
}

func NewClientWithConfig(cfg ClientConfig) *Client {
    return &Client{
        httpClient: cfg.HTTPClient,
        ...
    }
}
```

지금 bootstrap 주경로는 실제 client를 넣고 있어 운영에서는 안 터질 수 있다.
하지만 public constructor가 nil-safe하지 않으면 테스트, 별도 wiring, 향후 refactor에서 바로 panic 난다.

또한 아래 함수들이 사실상 같은 네트워크 뼈대를 반복한다.

- `GetLiveStatus`
- `GetScheduledLives`
- `GetLives`
- `GetChannels`

중복되는 것은 단순 몇 줄이 아니라,

- circuit check
- request 생성
- HTTP do
- status 처리
- body read limit
- JSON unmarshal
- circuit reset

전체 플로우다. 이런 중복은 버그 수정이 한쪽만 반영되는 전형적인 출발점이다.

#### 권장 패치 A: 생성자에서 기본 external API client 보장

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go b/hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go
@@
 import (
 	context
 	"errors"
 	"fmt"
 	"log/slog"
 	"net/http"
 	"net/url"
 	"slices"
 	"strings"
 	"sync"
 	"time"
 
 	"github.com/kapu/hololive-shared/pkg/constants"
 	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
+	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
 	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
@@
 func NewClient(httpClient *http.Client, baseURL string, logger *slog.Logger) *Client {
+	if httpClient == nil {
+		httpClient = httputil.NewExternalAPIClient(10 * time.Second)
+	}
+	if strings.TrimSpace(baseURL) == "" {
+		baseURL = DefaultBaseURL
+	}
+
 	return &Client{
 		httpClient:     httpClient,
 		baseURL:        baseURL,
@@
 func NewClientWithConfig(cfg ClientConfig) *Client {
 	baseURL := cfg.BaseURL
 	if baseURL == "" {
 		baseURL = DefaultBaseURL
 	}
+	httpClient := cfg.HTTPClient
+	if httpClient == nil {
+		httpClient = httputil.NewExternalAPIClient(10 * time.Second)
+	}
 
 	return &Client{
-		httpClient:     cfg.HTTPClient,
+		httpClient:     httpClient,
 		baseURL:        baseURL,
```

#### 권장 패치 B: 공통 request executor 추출

```diff
@@
 func (c *Client) newRequest(ctx context.Context, method, targetURL string) (*http.Request, error) {
@@
 }
+
+func (c *Client) doRequest(op string, req *http.Request) ([]byte, error) {
+	resp, err := c.httpClient.Do(req)
+	if err != nil {
+		c.handleRequestFailure()
+		return nil, &apperrors.APIError{Operation: op, StatusCode: 0, Err: err}
+	}
+	defer func() { _ = resp.Body.Close() }()
+
+	if resp.StatusCode != http.StatusOK {
+		c.handleStatusCodeError(resp.StatusCode)
+		return nil, &apperrors.APIError{Operation: op, StatusCode: resp.StatusCode}
+	}
+
+	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
+	if err != nil {
+		return nil, fmt.Errorf("read response body: %w", err)
+	}
+
+	c.resetCircuit()
+	return body, nil
+}
```

`GetLiveStatus()` 예시는 다음처럼 줄어든다.

```diff
@@
 	req, err := c.newRequest(ctx, "GET", reqURL)
 	if err != nil {
 		return nil, fmt.Errorf("failed to create request: %w", err)
 	}
-
-	resp, err := c.httpClient.Do(req)
-	if err != nil {
-		c.handleRequestFailure()
-
-		return nil, &apperrors.APIError{
-			Operation:  chzzkGetLiveStatusOp,
-			StatusCode: 0,
-			Err:        err,
-		}
-	}
-
-	defer func() { _ = resp.Body.Close() }()
-
-	if resp.StatusCode != http.StatusOK {
-		c.handleStatusCodeError(resp.StatusCode)
-
-		return nil, &apperrors.APIError{
-			Operation:  chzzkGetLiveStatusOp,
-			StatusCode: resp.StatusCode,
-		}
-	}
-
-	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
+	body, err := c.doRequest(chzzkGetLiveStatusOp, req)
 	if err != nil {
-		return nil, fmt.Errorf("failed to read response body: %w", err)
+		return nil, err
 	}
@@
-	c.resetCircuit()
 	return liveStatusResp.Content, nil
 }
```

`GetScheduledLives`, `GetLives`, `GetChannels`도 동일 패턴으로 교체하면 된다.

#### 추가 테스트

- nil httpClient로 constructor 호출 시 panic 없이 기본 client 생성되는지
- `doRequest()`를 기준으로 200/500/network error/body too large를 table-driven test로 검증

#### 우선순위

**중상**. 당장 대형 장애보다 “한 번 틀리면 여기저기 같이 틀리는” 코드다.

---

### 2.5 `youtube/poller`: map iteration 순서가 그대로 배치 update 순서로 흘러간다

대상:
- `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch_alarm_state.go:10-68`
- `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch.go:271-308`

`buildCommunityShortsAlarmStates()`는 `rowsByKey map[string]*...`를 만든 뒤 그대로 range 한다.

```go
rows := make([]*domain.YouTubeCommunityShortsAlarmState, 0, len(rowsByKey))
for _, row := range rowsByKey {
    ...
    rows = append(rows, row)
}
```

Go map iteration은 비결정적이다.
이 자체는 잘못이 아니지만, 문제는 이 값이 나중에 batch upsert 입력 순서로 이어질 때다.

순서가 매번 바뀌면 다음이 발생한다.

- 테스트가 flaky 해질 수 있음
- SQL batch order가 흔들림
- 동일 키 다건 upsert 시 deadlock surface가 불필요하게 커짐
- 로그/디버깅 재현성이 낮아짐

`collectShortIdentityAliases()`도 같은 문제가 있다.

```go
aliases := make([]string, 0, len(aliasSet))
for alias := range aliasSet {
    aliases = append(aliases, alias)
}
```

#### 권장 패치 A: alarm state rows 정렬

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/poller/repository_batch_alarm_state.go b/hololive/hololive-shared/pkg/service/youtube/poller/repository_batch_alarm_state.go
@@
 import (
+	"sort"
 	"strings"
@@
-	rows := make([]*domain.YouTubeCommunityShortsAlarmState, 0, len(rowsByKey))
-	for _, row := range rowsByKey {
+	keys := make([]string, 0, len(rowsByKey))
+	for key := range rowsByKey {
+		keys = append(keys, key)
+	}
+	sort.Strings(keys)
+
+	rows := make([]*domain.YouTubeCommunityShortsAlarmState, 0, len(keys))
+	for _, key := range keys {
+		row := rowsByKey[key]
 		if row == nil {
 			continue
 		}
 		row.DeliveryStatus = domain.ResolveYouTubeCommunityShortsAlarmStateStatus(row.AuthorizedAt, row.AlarmSentAt)
 		rows = append(rows, row)
 	}
```

#### 권장 패치 B: alias도 정렬

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/poller/repository_batch.go b/hololive/hololive-shared/pkg/service/youtube/poller/repository_batch.go
@@
 import (
 	context
 	"fmt"
+	"sort"
@@
 	aliases := make([]string, 0, len(aliasSet))
 	for alias := range aliasSet {
 		aliases = append(aliases, alias)
 	}
+	sort.Strings(aliases)
 	return canonicalIDs, aliases
 }
```

#### 우선순위

**중상**. 기능이 틀린다는 뜻보다, 운영/테스트 재현성이 떨어지는 유형이다. 빨리 고치는 것이 좋다.

---

### 2.6 `member/repository.go`: 큰 파일, 중복, 그리고 일부 경로의 조용한 partial success가 아직 남아 있다

대상:
- `hololive/hololive-shared/pkg/service/member/repository.go:76-197`
- `hololive/hololive-shared/pkg/service/member/repository.go:367-404`
- `hololive/hololive-shared/pkg/service/member/repository.go:706-724`
- `hololive/hololive-shared/pkg/service/member/repository.go:728-827`

이 파일은 이전보다 나아졌다.
특히 aggregate helper 경로는 `errors.Join(rowErrs...)`를 반환하도록 개선되었다.

하지만 아직 두 문제가 남아 있다.

1. **단건 query 함수들이 거의 복붙 수준으로 반복됨**
2. `FindAllByName()`는 아직도 scan/parse 실패를 warn만 남기고 `continue` 하며 partial success를 반환함

현재 `FindAllByName()`:

```go
if err := rows.Scan(...); err != nil {
    r.logger.Warn("Failed to scan member row", slog.Any("error", err))
    continue
}
...
if err != nil {
    r.logger.Warn("Failed to parse member", slog.String("name", englishName), slog.Any("error", err))
    continue
}
```

이건 사용자가 “검색 결과가 일부 비어 보이는” 현상으로 나타나고, 원인은 로그를 뒤져야만 알 수 있다.
이런 repository는 API가 아니라 **데이터 손상 은폐기**가 된다.

#### 권장 패치 A: `FindAllByName()`도 row error를 모아 반환

```diff
diff --git a/hololive/hololive-shared/pkg/service/member/repository.go b/hololive/hololive-shared/pkg/service/member/repository.go
@@
 	var members []*domain.Member
+	var rowErrs []error
 	for rows.Next() {
@@
 		if err := rows.Scan(&id, &slug, &channelID, &englishName, &japaneseName, &koreanName,
 			&status, &isGraduated, &aliasesJSON, &org, &suborg, &syncSource, &twitchUserID); err != nil {
-			r.logger.Warn("Failed to scan member row", slog.Any("error", err))
+			rowErrs = append(rowErrs, fmt.Errorf("failed to scan member row: %w", err))
 			continue
 		}
@@
 		member, err := r.scanMember(id, slug, channelID, englishName, japaneseName, koreanName, status, isGraduated, aliasesJSON, nil, org, suborg, syncSource, twitchUserID)
 		if err != nil {
-			r.logger.Warn("Failed to parse member", slog.String("name", englishName), slog.Any("error", err))
+			rowErrs = append(rowErrs, fmt.Errorf("failed to parse member row %q: %w", englishName, err))
 			continue
 		}
@@
 	if err := rows.Err(); err != nil {
-		return nil, fmt.Errorf("rows iteration error: %w", err)
+		rowErrs = append(rowErrs, fmt.Errorf("rows iteration error: %w", err))
 	}
 
-	return members, nil
+	if len(rowErrs) > 0 {
+		return members, errors.Join(rowErrs...)
+	}
+
+	return members, nil
 }
```

#### 권장 패치 B: 수동 문자열 helper 제거

`contains`, `findSubstring`, `replaceFirst`는 표준 라이브러리로 대체해야 한다.
지금은 `strings.Contains`, `strings.Replace`가 더 명확하고, 커스텀 구현은 유지비만 만든다.

```diff
@@
-func contains(s, substr string) bool {
-    return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr) >= 0)
-}
-
-func findSubstring(s, substr string) int {
-    for i := 0; i <= len(s)-len(substr); i++ {
-        if s[i:i+len(substr)] == substr {
-            return i
-        }
-    }
-    return -1
-}
-
-func replaceFirst(s, old, replacement string) string {
-    idx := findSubstring(s, old)
-    if idx < 0 {
-        return s
-    }
-    return s[:idx] + replacement + s[idx+len(old):]
-}
+// replaceFirst가 필요하면 strings.Replace(..., 1) 사용.
```

호출부는 아래처럼 바꾼다.

```diff
- if contains(photoURL, "=s96-c") {
-     photoURL = replaceFirst(photoURL, "=s96-c", "=s256-c")
+ if strings.Contains(photoURL, "=s96-c") {
+     photoURL = strings.Replace(photoURL, "=s96-c", "=s256-c", 1)
 }
```

#### 권장 패치 C: 단건 query helper 추출

아래 helper를 추가한다.

```go
type memberRow struct {
    id           int
    slug         string
    channelID    *string
    englishName  string
    japaneseName *string
    koreanName   *string
    status       string
    isGraduated  bool
    aliasesJSON  []byte
    photo        *string
    org          string
    suborg       *string
    syncSource   string
    twitchUserID *string
}

func (r *Repository) scanMemberRow(row *memberRow) (*domain.Member, error) {
    if row == nil {
        return nil, nil
    }
    return r.scanMember(
        row.id,
        row.slug,
        row.channelID,
        row.englishName,
        row.japaneseName,
        row.koreanName,
        row.status,
        row.isGraduated,
        row.aliasesJSON,
        row.photo,
        row.org,
        row.suborg,
        row.syncSource,
        row.twitchUserID,
    )
}

func (r *Repository) querySingleMember(ctx context.Context, query string, args ...any) (*domain.Member, error) {
    var row memberRow
    err := r.pool.QueryRow(ctx, query, args...).Scan(
        &row.id,
        &row.slug,
        &row.channelID,
        &row.englishName,
        &row.japaneseName,
        &row.koreanName,
        &row.status,
        &row.isGraduated,
        &row.aliasesJSON,
        &row.org,
        &row.suborg,
        &row.syncSource,
        &row.twitchUserID,
    )
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    return r.scanMemberRow(&row)
}
```

그리고 아래 함수들은 전부 helper 기반으로 축소한다.

- `FindByChannelID`
- `FindByName`
- `FindByAlias`
- `FindByNameAndOrg`
- `GetMemberWithPhotoByChannelID`는 photo 포함용 별도 helper 하나 추가

`FindByName` 최종 형태 예시:

```go
func (r *Repository) FindByName(ctx context.Context, name string) (*domain.Member, error) {
    query := `
        SELECT id, slug, channel_id, english_name, japanese_name, korean_name,
               status, is_graduated, aliases, org, suborg, sync_source, twitch_user_id
        FROM members
        WHERE english_name = $1
        LIMIT 1
    `

    member, err := r.querySingleMember(ctx, query, name)
    if err != nil {
        return nil, fmt.Errorf("failed to query member by name: %w", err)
    }
    return member, nil
}
```

#### 우선순위

**중상**. 지금 당장 서비스가 깨지는 파일은 아니지만, 유지보수와 데이터 진단 난도를 계속 올린다.

---

### 2.7 `internal/app`의 thin wrapper 계층은 AI 냄새가 강하고, 스스로 적은 원칙과 충돌한다

대상:
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_types.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_providers.go`
- `hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer.go`
- `hololive/hololive-kakao-bot-go/internal/app/providers_alarm_consumers.go`
- `hololive/hololive-kakao-bot-go/internal/app/README.md:7-9`

`internal/app/README.md`에는 이렇게 적혀 있다.

- 루트 `internal/app` 는 façade / orchestration 역할만 유지한다.
- 구현은 `internal/app/.../bootstrap` 등 아래로 내린다.
- **얇은 중복 wrapper 와 불필요한 추가 테스트 파일 누적을 피한다.**

그런데 실제로는 아래처럼 “그냥 한 번 더 감싼 함수”가 남아 있다.

```go
func ProvideACLService(...) (*acl.Service, error) {
    return appbootstrap.ProvideACLService(...)
}
```

또 type alias 파일도 남아 있다.

```go
type coreInfrastructure = appbootstrap.CoreInfrastructure
```

이런 파일은 런타임 리스크는 낮지만, 구조 관점에서는 명백히 노이즈다.
특히 테스트도 wrapper 레이어를 한 번 더 고정시키므로, 구조 정리를 어렵게 만든다.

#### 권장 패치

1. 아래 파일 삭제
   - `bootstrap_services_types.go`
   - `bootstrap_services_providers.go`
   - `providers_single_consumer.go`
   - `providers_alarm_consumers.go`

2. call site를 `appbootstrap` 직접 참조로 변경

예시:

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_modules.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_modules.go
@@
-import (
+import (
     ...
+    appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
 )
@@
-    chzzkClient := ProvideChzzkClient(httpClient, cfg.Chzzk, logger)
+    chzzkClient := appbootstrap.ProvideChzzkClient(httpClient, cfg.Chzzk, logger)
```

`botDependencyModules`, `coreInfrastructure` 같은 alias는 호출부에서 `appbootstrap.BotDependencyModules`, `appbootstrap.CoreInfrastructure`로 직접 바꾼다.

3. wrapper 전용 테스트는 삭제하거나 bootstrap 패키지 테스트로 흡수

#### 우선순위

**중**. 운영장애 직결은 아니지만, 구조 일관성과 코드 소음을 줄이는 데 의미가 크다.

---

### 2.8 `cache/service.go`: sub-second TTL이 0초로 깎일 수 있다

대상:
- `hololive/hololive-shared/pkg/service/cache/service.go:170-223`
- `hololive/hololive-shared/pkg/service/cache/service.go:425-430`
- `hololive/hololive-shared/pkg/service/cache/service.go:491-497`

`Set`, `MSet`, `Expire`, `SetNX`가 모두 `int64(ttl.Seconds())`를 사용한다.

```go
ExSeconds(int64(ttl.Seconds()))
Seconds(int64(ttl.Seconds()))
```

문제는 `ttl = 500 * time.Millisecond` 같은 값이 오면 `0`으로 잘린다는 점이다.
공유 cache 라이브러리에서 이런 truncation은 latent bug다.
현재 호출부가 대부분 초 단위여도, 라이브러리 수준에서는 막아두는 편이 맞다.

#### 권장 패치

`cache/service.go`에 helper를 추가하고 전부 그것을 쓰게 바꾼다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/cache/service.go b/hololive/hololive-shared/pkg/service/cache/service.go
@@
 func (c *Service) Get(ctx context.Context, key string, dest any) error {
@@
 }
+
+func ttlSecondsCeil(ttl time.Duration) (int64, error) {
+	if ttl < 0 {
+		return 0, fmt.Errorf("ttl must not be negative")
+	}
+	if ttl == 0 {
+		return 0, nil
+	}
+	seconds := int64(math.Ceil(ttl.Seconds()))
+	if seconds <= 0 {
+		seconds = 1
+	}
+	return seconds, nil
+}
@@
 	var cmd valkey.Completed
 	if ttl > 0 {
-		cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(int64(ttl.Seconds())).Build()
+		ttlSeconds, err := ttlSecondsCeil(ttl)
+		if err != nil {
+			return NewCacheError("invalid ttl", "set", key, err)
+		}
+		cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(ttlSeconds).Build()
 	} else {
@@
 			if ttl > 0 {
-				cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(int64(ttl.Seconds())).Build()
+				ttlSeconds, err := ttlSecondsCeil(ttl)
+				if err != nil {
+					return NewCacheError("invalid ttl", "mset", key, err)
+				}
+				cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(ttlSeconds).Build()
 			} else {
@@
 func (c *Service) Expire(ctx context.Context, key string, ttl time.Duration) error {
-	if err := c.client.Do(ctx, c.client.B().Expire().Key(key).Seconds(int64(ttl.Seconds())).Build()).Error(); err != nil {
+	ttlSeconds, err := ttlSecondsCeil(ttl)
+	if err != nil {
+		return NewCacheError("invalid ttl", "expire", key, err)
+	}
+	if err := c.client.Do(ctx, c.client.B().Expire().Key(key).Seconds(ttlSeconds).Build()).Error(); err != nil {
 		c.logger.Error("Cache expire failed", slog.String("key", key), slog.Any("error", err))
 		return NewCacheError("expire failed", "expire", key, err)
 	}
@@
 	var cmd valkey.Completed
 	if ttl > 0 {
-		cmd = c.client.B().Set().Key(key).Value(value).Nx().ExSeconds(int64(ttl.Seconds())).Build()
+		ttlSeconds, err := ttlSecondsCeil(ttl)
+		if err != nil {
+			return false, NewCacheError("invalid ttl", "setnx", key, err)
+		}
+		cmd = c.client.B().Set().Key(key).Value(value).Nx().ExSeconds(ttlSeconds).Build()
 	} else {
```

#### 우선순위

**중**. 즉시 터지는 버그는 아니지만 shared library로서는 남겨두면 안 된다.

---

## 3. 중간 우선순위의 code duplication / god object 리팩토링 후보

아래는 “당장 오늘 장애” 급은 아니지만, 다음 버그의 산실이 될 확률이 높은 곳이다.

### 3.1 `youtube/stats/stats_repository_read.go`

대상:
- `getLatestStatsForChannelsFromHistory`
- `getLatestStatsForChannelsFromSnapshot`

두 함수는 row scan/aggregation 본문이 거의 같다.
쿼리만 다르고 나머지는 사실상 복붙이다.

#### 권장 리팩토링

- 공통 helper `scanTimestampedStatsRows(rows pgx.Rows, logger *slog.Logger, capacity int) (map[string]*domain.TimestampedStats, error)` 추출
- 각 함수는 query만 수행하고 helper 호출

효과:
- scan 에러 정책, member name 처리, rows.Err 처리 일관화
- snapshot/history 분기에서 버그 수정 누락 방지

---

### 3.2 `youtube/stats/stats_repository_milestone.go`

대상:
- `GetNearMilestoneMembers` (`:224`)
- `GetClosestMilestoneMembers` (`:330`)

이 둘은 CTE 구조와 scan 루프가 상당히 비슷하다.
중복 SQL 전체를 억지로 generic화할 필요는 없지만, 최소한 아래는 추출해야 한다.

- `scanNearMilestoneRows(rows pgx.Rows) ([]NearMilestoneEntry, error)`
- 공통 CTE prefix builder

---

### 3.3 `membernews` weekly/monthly scheduler

대상:
- `hololive/hololive-llm-sched/internal/service/membernews/scheduler/scheduler.go`
- `hololive/hololive-llm-sched/internal/service/membernews/scheduler/monthly_scheduler.go`

`SendWeeklyDigest()`와 `SendMonthlyDigest()`는 잠금, room fan-out, semaphore, result merge 구조가 거의 동일하다.
차이는 다음 정도다.

- period key 생성기
- lock key prefix
- delivery kind
- 로그 문구

#### 권장 리팩토링

`runDigestDispatch(ctx, cfg digestDispatchConfig) error` 형태로 추출

```go
type digestDispatchConfig struct {
    periodLabel string
    periodKey   string
    lockKey     string
    title       string
    kind        domain.DeliveryKind
    processRoom func(context.Context, string, string) delivery.SendResult
}
```

이렇게 하면 weekly/monthly 모두 thin wrapper가 되고, concurrency/lock/error 정책이 한 곳에 모인다.

---

## 4. `admin-dashboard`에 대한 코멘트

이번 번들에서는 지난번에 지적했던 auth/session refresh payload 불일치 문제는 해결된 것으로 보인다.
다만 Rust 쪽에 남아 있는 작은 구조 문제는 있다.

### 4.1 runtime constructor의 `.expect(...)`

대상:
- `admin-dashboard/backend/src/status/collector.rs:37-48`
- `admin-dashboard/backend/src/status/system_stats.rs:56-63`

현재는 runtime HTTP client 생성 실패 시 panic/abort 성격이다.

```rust
let http_client = Client::builder()
    .timeout(Duration::from_secs(3))
    .build()
    .expect("http client");
```

이건 희귀한 실패라 하더라도, 운영 서버에서는 panic보다 `Result` 또는 log-and-disable이 낫다.

#### 권장 패치

`StatusCollector::new()`는 `Result<Self, anyhow::Error>`로 바꾸고,
`SystemStatsCollector::start()`는 spawn 전에 client를 만들되 실패하면 error log 후 collector를 비활성화한다.

```diff
diff --git a/admin-dashboard/backend/src/status/collector.rs b/admin-dashboard/backend/src/status/collector.rs
@@
 impl StatusCollector {
-    pub fn new(endpoints: Vec<ServiceEndpoint>, version: &str) -> Self {
+    pub fn new(endpoints: Vec<ServiceEndpoint>, version: &str) -> anyhow::Result<Self> {
         let http_client = Client::builder()
             .timeout(Duration::from_secs(3))
             .build()
-            .expect("http client");
+            .context("build status collector http client")?;
 
-        Self {
+        Ok(Self {
             http_client,
             endpoints,
             start_time: Instant::now(),
             version: version.to_string(),
-        }
+        })
     }
 }
```

`system_stats.rs`는 아래 방향으로 수정한다.

```rust
let http_client = match Client::builder().timeout(Duration::from_secs(2)).build() {
    Ok(client) => client,
    Err(err) => {
        tracing::error!(error = %err, "failed to build system stats http client");
        return;
    }
};
```

우선순위는 높지 않지만, 운영 프로세스가 panic에 덜 의존하게 된다.

---

## 5. 전체 우선순위 정리

### 1순위: 바로 패치

1. `alarm/targets.go` deadline-preserving detach
2. `bot_message_async.go` goroutine fallback 제거
3. `auth/service.go` session index rollback + refresh cleanup 보장
4. `auth/service.go` atomic `incrWithTTL`

### 2순위: 이번 배포 주기 안에 처리

5. `chzzk/client.go` nil httpClient 기본값 + request helper 추출
6. `youtube/poller` deterministic sort
7. `cache/service.go` TTL ceil normalization

### 3순위: 구조 정리 스프린트에서 처리

8. `member/repository.go` helper 추출 + partial success 제거
9. `internal/app` thin wrapper 삭제
10. `stats repository`, `membernews scheduler` 중복 정리
11. `admin-dashboard` runtime `.expect` 축소

---

## 6. 최종 판단

이번 번들은 이전보다 분명히 좋아졌다.
특히 **이전 리뷰에서 치명적이었던 문제를 실제로 고쳤다**는 점은 높게 평가할 만하다.

하지만 지금 남아 있는 문제들도 성격이 가볍지 않다.
이번 번들의 남은 핵심 결함은 “중복 코드가 좀 많다” 수준이 아니라 다음과 같다.

- 요청은 끝났는데 DB 작업은 더 오래 살아남을 수 있다
- 부하가 걸릴수록 worker pool 보호장치를 우회한다
- 인증 세션 인덱스 실패가 보안 의미를 깨뜨릴 수 있다
- 비결정성 때문에 테스트/운영 재현성이 낮다

즉, 이번 번들은 **지난번보다 훨씬 건강해졌지만, 아직 ‘운영 의미’ 기준으로 몇 군데 더 닫아야 한다.**

이 문서 기준으로 바로 패치할 때 가장 추천하는 순서는 다음이다.

1. `alarm/targets.go`
2. `bot_message_async.go`
3. `auth/service.go`
4. `chzzk/client.go`
5. `youtube/poller` 정렬
6. `cache/service.go` TTL 정규화
7. `member/repository.go`
8. thin wrapper 제거

여기까지 하면 이번 번들은 “이전보다 좋아진 코드”를 넘어서 **실패 의미가 훨씬 선명한 코드**로 올라간다.
