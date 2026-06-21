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
	"testing"

	"github.com/gin-gonic/gin"
	json "github.com/park285/shared-go/pkg/json"
)

func TestPanicLog_HasRequestID_23350deb(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	router := gin.New()
	router.Use(RecoveryMiddleware(context.Background(), logger))
	router.Use(RequestIDMiddleware())
	router.GET("/boom", func(_ *gin.Context) {
		panic("boom")
	})

	const wantReqID = "req-23350deb"
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/boom", http.NoBody)
	req.Header.Set("X-Request-ID", wantReqID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("로그 JSON 파싱 실패: %v, raw=%s", err, buf.String())
	}

	if got := entry["msg"]; got != "http.request.panic_recovered" {
		t.Fatalf("msg = %v, want http.request.panic_recovered", got)
	}
	if got, ok := entry["request_id"].(string); !ok || got != wantReqID {
		t.Fatalf("request_id = %v, want %q", entry["request_id"], wantReqID)
	}
	if got := entry["method"]; got != http.MethodGet {
		t.Fatalf("method = %v, want %v", got, http.MethodGet)
	}
	if got := entry["path"]; got != "/boom" {
		t.Fatalf("path = %v, want /boom", got)
	}
}
