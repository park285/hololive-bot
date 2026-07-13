// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package middleware

import (
	"context"
	"encoding/json"
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
			name:       "빈 API 키는 인증 미설정으로 차단",
			apiKey:     "",
			headerVal:  "",
			wantStatus: http.StatusServiceUnavailable,
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

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", http.NoBody)
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

func TestAPIKeyAuthMiddleware_ResponseBodyContract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(APIKeyAuthMiddleware("test-key"))
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := payload["error"]; got != "unauthorized" {
		t.Fatalf("error = %v, want %q", got, "unauthorized")
	}
	if got := payload["message"]; got != "API key required" {
		t.Fatalf("message = %v, want %q", got, "API key required")
	}
}

func TestAuthMiddlewareExplicitDisabled(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(AuthMiddleware(AuthConfig{Disabled: true}))
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
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
			name:       "빈 API 키는 NoRoute에서도 인증 미설정으로 차단",
			apiKey:     "",
			headerVal:  "",
			wantStatus: http.StatusServiceUnavailable,
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

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/nonexistent", http.NoBody)
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

func TestNoRouteAuthHandler_ResponseBodyContract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.NoRoute(NoRouteAuthHandler("test-key"))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/nonexistent", http.NoBody)
	req.Header.Set(APIKeyHeader, "wrong-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := payload["error"]; got != "forbidden" {
		t.Fatalf("error = %v, want %q", got, "forbidden")
	}
	if got := payload["message"]; got != "invalid API key" {
		t.Fatalf("message = %v, want %q", got, "invalid API key")
	}
}

func TestNoRouteHandlerExplicitDisabled(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.NoRoute(NoRouteHandler(AuthConfig{Disabled: true}))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
