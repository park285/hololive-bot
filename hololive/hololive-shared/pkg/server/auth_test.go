package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIKeyAuthMiddleware(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		apiKey     string // 미들웨어에 설정된 키
		headerVal  string // 요청 헤더에 담는 값 ("" = 헤더 미전송)
		wantStatus int
	}{
		{
			name:       "빈 API 키(개발 모드): 헤더 없어도 통과",
			apiKey:     "",
			headerVal:  "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "유효한 키 설정 시 헤더 미전송 → 401",
			apiKey:     "test-key",
			headerVal:  "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "잘못된 키 전송 → 403",
			apiKey:     "test-key",
			headerVal:  "wrong-key",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "올바른 키 전송 → 200",
			apiKey:     "test-key",
			headerVal:  "test-key",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router := gin.New()
			router.Use(APIKeyAuthMiddleware(tt.apiKey))
			router.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.headerVal != "" {
				req.Header.Set(APIKeyHeader, tt.headerVal)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestNoRouteAuthHandler(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		apiKey     string // 핸들러에 설정된 키
		headerVal  string // 요청 헤더에 담는 값 ("" = 헤더 미전송)
		wantStatus int
	}{
		{
			name:       "빈 API 키(개발 모드): 등록되지 않은 경로 → 404",
			apiKey:     "",
			headerVal:  "",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "유효한 키 설정 시 헤더 미전송 → 401",
			apiKey:     "test-key",
			headerVal:  "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "잘못된 키 전송 → 403",
			apiKey:     "test-key",
			headerVal:  "wrong-key",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "올바른 키 전송이지만 경로 없음 → 404",
			apiKey:     "test-key",
			headerVal:  "test-key",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router := gin.New()
			// NoRoute 핸들러만 등록 (실제 라우트 없음)
			router.NoRoute(NoRouteAuthHandler(tt.apiKey))

			req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
			if tt.headerVal != "" {
				req.Header.Set(APIKeyHeader, tt.headerVal)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
