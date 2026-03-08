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
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

func TestShouldSkipPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		exactSkip  map[string]bool
		prefixSkip []string
		suffixSkip []string
		want       bool
	}{
		{
			name:      "정확히 일치 → true",
			path:      "/health",
			exactSkip: map[string]bool{"/health": true},
			want:      true,
		},
		{
			name:       "prefix 일치 → true",
			path:       "/api/holo/ws/connect",
			prefixSkip: []string{"/api/holo/ws"},
			want:       true,
		},
		{
			name:       "suffix 일치 → true",
			path:       "/api/stream",
			suffixSkip: []string{"/stream"},
			want:       true,
		},
		{
			name:       "어떤 패턴에도 불일치 → false",
			path:       "/api/data",
			exactSkip:  map[string]bool{"/health": true},
			prefixSkip: []string{"/metrics"},
			suffixSkip: []string{"/stream"},
			want:       false,
		},
		{
			name:       "빈 맵/슬라이스 → false",
			path:       "/anything",
			exactSkip:  map[string]bool{},
			prefixSkip: []string{},
			suffixSkip: []string{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldSkipPath(tt.path, tt.exactSkip, tt.prefixSkip, tt.suffixSkip)
			if got != tt.want {
				t.Fatalf("shouldSkipPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestTruncateUA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ua   string
		want string
	}{
		{
			name: "짧은 UA → 그대로 반환",
			ua:   "Mozilla/5.0",
			want: "Mozilla/5.0",
		},
		{
			name: "정확히 80자 → 그대로 반환",
			ua:   strings.Repeat("a", 80),
			want: strings.Repeat("a", 80),
		},
		{
			name: "81자 초과 → 80자 잘라 '...' 추가",
			ua:   strings.Repeat("b", 100),
			want: strings.Repeat("b", 80) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := truncateUA(tt.ua)
			if got != tt.want {
				t.Fatalf("truncateUA(%q) = %q, want %q", tt.ua, got, tt.want)
			}
		})
	}
}

func TestLogDebugf_NoPanic(t *testing.T) {
	t.Parallel()

	// 패닉 없이 실행되는지만 검증 (스모크 테스트)
	logger := slog.Default()
	ctx := context.Background()

	// 패닉 발생 시 테스트가 실패하므로 별도 assertion 불필요
	LogDebugf(ctx, logger, "테스트 메시지", slog.String("key", "value"))
}

func TestLoggerMiddleware_IncludesRequestSourceFields(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.ReleaseMode)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	router := gin.New()
	if err := router.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatalf("SetTrustedProxies 에러: %v", err)
	}
	router.Use(LoggerMiddleware(context.Background(), logger))
	router.GET("/inspect", func(c *gin.Context) {
		c.Status(http.StatusUnauthorized)
	})

	req := httptest.NewRequest(http.MethodGet, "/inspect", nil)
	req.RemoteAddr = "10.10.0.5:4321"
	req.Header.Set("User-Agent", "curl/8.5.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("X-Real-IP", "203.0.113.11")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("로그 JSON 파싱 실패: %v, raw=%s", err, buf.String())
	}

	if got := entry["msg"]; got != "HTTP" {
		t.Fatalf("msg = %v, want HTTP", got)
	}
	if got := entry["ip"]; got != "203.0.113.10" {
		t.Fatalf("ip = %v, want 203.0.113.10", got)
	}
	if got := entry["remote_addr"]; got != "10.10.0.5:4321" {
		t.Fatalf("remote_addr = %v, want 10.10.0.5:4321", got)
	}
	if got := entry["x_forwarded_for"]; got != "203.0.113.10" {
		t.Fatalf("x_forwarded_for = %v, want 203.0.113.10", got)
	}
	if got := entry["x_real_ip"]; got != "203.0.113.11" {
		t.Fatalf("x_real_ip = %v, want 203.0.113.11", got)
	}
}
