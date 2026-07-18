package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server"
)

func newAuthRouter(t *testing.T) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	handler := server.NewAuthHandler(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	router := gin.New()
	router.POST("/api/auth/login", handler.Login)
	router.POST("/api/auth/refresh", handler.Refresh)
	router.GET("/api/auth/me", handler.Me)
	return router
}

func newAuthRequest(t *testing.T, method, path, body, authorization string) *http.Request {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return req
}

func assertAuthErrorBody(t *testing.T, body []byte, wantErrorCode string) {
	t.Helper()

	var parsed struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode response %q: %v", body, err)
	}
	if parsed.Success {
		t.Fatal("success = true, want false")
	}
	if parsed.Error != wantErrorCode {
		t.Fatalf("error code = %q, want %q", parsed.Error, wantErrorCode)
	}
}

func TestAuthHandlerErrorContract(t *testing.T) {
	router := newAuthRouter(t)

	for _, tc := range []struct {
		name          string
		method        string
		path          string
		body          string
		authorization string
		wantStatus    int
		wantErrorCode string
	}{
		{
			name:          "login rejects malformed json",
			method:        http.MethodPost,
			path:          "/api/auth/login",
			body:          "{",
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "INVALID_INPUT",
		},
		{
			name:          "login without auth service is unavailable",
			method:        http.MethodPost,
			path:          "/api/auth/login",
			body:          `{"email":"admin@example.com","password":"secret"}`,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "INTERNAL_ERROR",
		},
		{
			name:          "me requires bearer token",
			method:        http.MethodGet,
			path:          "/api/auth/me",
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "UNAUTHORIZED",
		},
		{
			name:          "refresh rejects non-bearer authorization",
			method:        http.MethodPost,
			path:          "/api/auth/refresh",
			authorization: "Token abc123",
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "UNAUTHORIZED",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, newAuthRequest(t, tc.method, tc.path, tc.body, tc.authorization))

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			assertAuthErrorBody(t, recorder.Body.Bytes(), tc.wantErrorCode)
		})
	}
}
