package middleware

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/httputil"
)

const testAPIKey = "test-key"

func TestAPIKeyAuthMiddleware(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		apiKey     string
		headerVal  string
		wantStatus int
	}{
		{
			name:       "빈 API 키는 인증 미설정으로 차단",
			apiKey:     "",
			headerVal:  "",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "유효한 키 설정 시 헤더 미전송 → 401",
			apiKey:     testAPIKey,
			headerVal:  "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "잘못된 키 전송 → 403",
			apiKey:     testAPIKey,
			headerVal:  "wrong-key",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "올바른 키 전송 → 200",
			apiKey:     testAPIKey,
			headerVal:  testAPIKey,
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

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", http.NoBody)

			if tt.headerVal != "" {
				req.Header.Set(httputil.HeaderAPIKey, tt.headerVal)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestAPIKeyAuthMiddleware_ResponseBodyContract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(APIKeyAuthMiddleware(testAPIKey))
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var payload map[string]any

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := payload["error"]; got != "unauthorized" {
		t.Fatalf("error = %v, want %q", got, "unauthorized")
	}

	if got := payload["message"]; got != "API key required" {
		t.Fatalf("message = %v, want %q", got, "API key required")
	}
}

func TestNoRouteAuthHandler(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		apiKey     string
		headerVal  string
		wantStatus int
	}{
		{
			name:       "빈 API 키는 NoRoute에서도 인증 미설정으로 차단",
			apiKey:     "",
			headerVal:  "",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "유효한 키 설정 시 헤더 미전송 → 401",
			apiKey:     testAPIKey,
			headerVal:  "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "잘못된 키 전송 → 403",
			apiKey:     testAPIKey,
			headerVal:  "wrong-key",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "올바른 키 전송이지만 경로 없음 → 404",
			apiKey:     testAPIKey,
			headerVal:  testAPIKey,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			router := gin.New()
			router.NoRoute(NoRouteAuthHandler(tt.apiKey))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nonexistent", http.NoBody)

			if tt.headerVal != "" {
				req.Header.Set(httputil.HeaderAPIKey, tt.headerVal)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestNoRouteAuthHandler_ResponseBodyContract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.NoRoute(NoRouteAuthHandler(testAPIKey))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nonexistent", http.NoBody)
	req.Header.Set(httputil.HeaderAPIKey, "wrong-key")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	var payload map[string]any

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := payload["error"]; got != "forbidden" {
		t.Fatalf("error = %v, want %q", got, "forbidden")
	}

	if got := payload["message"]; got != "invalid API key" {
		t.Fatalf("message = %v, want %q", got, "invalid API key")
	}
}
