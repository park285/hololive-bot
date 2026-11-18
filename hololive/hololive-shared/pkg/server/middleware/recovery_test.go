package middleware

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPanicLog_HasRequestID_23350deb(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	var buf bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	router := gin.New()
	router.Use(RecoveryMiddleware(t.Context(), logger))
	router.Use(RequestIDMiddleware())
	router.GET("/boom", func(_ *gin.Context) {
		panic("boom")
	})

	const wantReqID = "req-23350deb"

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/boom", http.NoBody)
	req.Header.Set("X-Request-ID", wantReqID)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var entry map[string]any

	if err := jsonv2.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
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
