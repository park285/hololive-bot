package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	internaltestutil "github.com/kapu/hololive-shared/internal/testutil"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// newTestCacheService: miniredis 기반 테스트용 캐시 서비스를 생성합니다.
func newTestCacheService(t *testing.T) cache.Client {
	t.Helper()
	return internaltestutil.NewTestCacheService(t, context.Background())
}

// newRateLimitRouter: RateLimitMiddleware가 적용된 테스트용 라우터를 반환합니다.
func newRateLimitRouter(cacheSvc cache.Client, limit int, window time.Duration, logger *slog.Logger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimitMiddleware(cacheSvc, limit, window, logger))
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

// doGet: 테스트용 GET 요청을 수행하고 응답 레코더를 반환합니다.
func doGet(r *gin.Engine, clientIP string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// X-Forwarded-For로 클라이언트 IP를 설정합니다 (gin.ClientIP() 동작용)
	if clientIP != "" {
		req.Header.Set("X-Forwarded-For", clientIP)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRateLimitMiddleware_NilCache_Passthrough(t *testing.T) {
	t.Parallel()

	// nil 캐시 → 미들웨어가 비활성화되고 요청은 항상 통과되어야 합니다.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := newRateLimitRouter(nil, 5, time.Minute, logger)

	rec := doGet(r, "10.0.0.1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (nil 캐시는 passthrough여야 합니다)", rec.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware_NilCache_NilLogger_Passthrough(t *testing.T) {
	t.Parallel()

	// nil 캐시 + nil 로거 → 패닉 없이 passthrough되어야 합니다.
	r := newRateLimitRouter(nil, 5, time.Minute, nil)

	rec := doGet(r, "10.0.0.2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (nil 캐시는 passthrough여야 합니다)", rec.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware_WithCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		limit      int
		requests   int  // 연속으로 보낼 요청 수
		wantLast   int  // 마지막 요청의 기대 상태 코드
		wantHeader bool // Retry-After 헤더 존재 여부 (rate limited 케이스)
	}{
		{
			name:       "limit=3, 3회 요청: 모두 허용 → 200",
			limit:      3,
			requests:   3,
			wantLast:   http.StatusOK,
			wantHeader: false,
		},
		{
			name:       "limit=2, 3회 요청: 초과 → 429",
			limit:      2,
			requests:   3,
			wantLast:   http.StatusTooManyRequests,
			wantHeader: true,
		},
		{
			name:       "limit=1, 2회 요청: 두 번째 → 429",
			limit:      1,
			requests:   2,
			wantLast:   http.StatusTooManyRequests,
			wantHeader: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cacheSvc := newTestCacheService(t)
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			r := newRateLimitRouter(cacheSvc, tt.limit, time.Minute, logger)

			clientIP := "192.168.1.100"
			var last *httptest.ResponseRecorder
			for i := range tt.requests {
				last = doGet(r, clientIP)
				_ = i
			}

			if last.Code != tt.wantLast {
				t.Fatalf("마지막 응답 status = %d, want %d", last.Code, tt.wantLast)
			}
			if tt.wantHeader {
				retryAfter := last.Header().Get("Retry-After")
				if retryAfter == "" {
					t.Fatal("Retry-After 헤더가 없습니다 (rate limited 응답에는 필수)")
				}
				// Retry-After 값이 양의 정수임을 확인합니다.
				secs, err := strconv.Atoi(retryAfter)
				if err != nil || secs < 1 {
					t.Fatalf("Retry-After = %q: 유효한 양의 정수여야 합니다", retryAfter)
				}
			}
		})
	}
}

func TestRateLimitMiddleware_DifferentIPs_IndependentBuckets(t *testing.T) {
	t.Parallel()

	// 서로 다른 IP는 독립적인 rate limit 버킷을 사용합니다.
	cacheSvc := newTestCacheService(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := newRateLimitRouter(cacheSvc, 1, time.Minute, logger)

	// IP A: 1회 요청 → 허용
	recA1 := doGet(r, "10.1.1.1")
	if recA1.Code != http.StatusOK {
		t.Fatalf("IP-A 첫 번째 요청: status = %d, want %d", recA1.Code, http.StatusOK)
	}

	// IP B: 1회 요청 → IP A와 별도 버킷이므로 허용되어야 합니다.
	recB1 := doGet(r, "10.2.2.2")
	if recB1.Code != http.StatusOK {
		t.Fatalf("IP-B 첫 번째 요청: status = %d, want %d (다른 IP는 독립 버킷)", recB1.Code, http.StatusOK)
	}

	// IP A: 2회 요청 → 한도 초과로 429
	recA2 := doGet(r, "10.1.1.1")
	if recA2.Code != http.StatusTooManyRequests {
		t.Fatalf("IP-A 두 번째 요청: status = %d, want %d", recA2.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimitMiddleware_RetryAfterIsPositive(t *testing.T) {
	t.Parallel()

	// rate limit 초과 시 Retry-After 값이 >= 1 임을 검증합니다.
	cacheSvc := newTestCacheService(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := newRateLimitRouter(cacheSvc, 1, time.Minute, logger)

	doGet(r, "10.10.10.10")        // 첫 번째 요청: 소진
	rec := doGet(r, "10.10.10.10") // 두 번째 요청: 초과

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("Retry-After 헤더가 없습니다")
	}
	secs, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After 파싱 실패: %v", err)
	}
	if secs < 1 {
		t.Fatalf("Retry-After = %d, 최솟값은 1이어야 합니다", secs)
	}
}
