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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeadersMiddleware_RemovesXXSSProtection(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(SecurityHeadersMiddleware())
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-XSS-Protection"); got != "" {
		t.Fatalf("X-XSS-Protection = %q, want empty", got)
	}

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

func TestSanitizedRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "uuid accepted", raw: "3f1a2b4c-5d6e-7f80-9012-3456789abcde", want: "3f1a2b4c-5d6e-7f80-9012-3456789abcde"},
		{name: "underscore dot colon accepted", raw: "svc_a.b:12", want: "svc_a.b:12"},
		{name: "empty rejected", raw: "", want: ""},
		{name: "crlf injection rejected", raw: "abc\r\nX-Admin: 1", want: ""},
		{name: "space rejected", raw: "abc def", want: ""},
		{name: "non ascii rejected", raw: "요청", want: ""},
		{name: "at length cap accepted", raw: strings.Repeat("a", maxRequestIDLength), want: strings.Repeat("a", maxRequestIDLength)},
		{name: "over length cap rejected", raw: strings.Repeat("a", maxRequestIDLength+1), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sanitizedRequestID(tt.raw); got != tt.want {
				t.Fatalf("sanitizedRequestID(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRequestIDMiddleware_ReissuesInvalidClientRequestID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestIDMiddleware())
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, c.GetString("request_id"))
	})

	tests := []struct {
		name     string
		header   string
		wantEcho bool
	}{
		{name: "valid client id is propagated", header: "client-req-1", wantEcho: true},
		{name: "injection attempt is reissued", header: "bad id\nSet-Cookie: x=1", wantEcho: false},
		{name: "oversized id is reissued", header: strings.Repeat("z", maxRequestIDLength+1), wantEcho: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
			req.Header.Set(requestIDHeaderKey, tt.header)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			got := rec.Body.String()
			if got == "" {
				t.Fatal("request_id must never be empty")
			}

			if tt.wantEcho && got != tt.header {
				t.Fatalf("request_id = %q, want propagated %q", got, tt.header)
			}

			if !tt.wantEcho && got == tt.header {
				t.Fatalf("request_id = %q, want a reissued value", got)
			}

			if header := rec.Header().Get(requestIDHeaderKey); header != got {
				t.Fatalf("X-Request-ID header = %q, want %q", header, got)
			}
		})
	}
}
