package server

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
)

func TestNewAuthHandler(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	if h == nil {
		t.Fatal("NewAuthHandler returned nil")
	}
	if h.auth != nil {
		t.Fatal("expected nil auth service")
	}
}

func TestParseBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		header string
		want   string
		ok     bool
	}{
		{name: "empty", header: "", want: "", ok: false},
		{name: "missing token", header: "Bearer", want: "", ok: false},
		{name: "wrong scheme", header: "Token abc", want: "", ok: false},
		{name: "valid", header: "Bearer abc", want: "abc", ok: true},
		{name: "valid case-insensitive", header: "bearer xyz", want: "xyz", ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			ctx.Request = req

			got, ok := parseBearerToken(ctx)
			if ok != tt.ok {
				t.Fatalf("ok=%v want=%v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("token=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestMapAuthErrorToHTTP(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   authsvc.ErrorCode
	}{
		{name: "non-auth error", err: errors.New("boom"), wantStatus: http.StatusInternalServerError, wantCode: authsvc.CodeInternal},
		{name: "invalid input", err: &authsvc.Error{Code: authsvc.CodeInvalidInput}, wantStatus: http.StatusBadRequest, wantCode: authsvc.CodeInvalidInput},
		{name: "email exists", err: &authsvc.Error{Code: authsvc.CodeEmailExists}, wantStatus: http.StatusConflict, wantCode: authsvc.CodeEmailExists},
		{name: "invalid credentials", err: &authsvc.Error{Code: authsvc.CodeInvalidCredentials}, wantStatus: http.StatusUnauthorized, wantCode: authsvc.CodeInvalidCredentials},
		{name: "account locked", err: &authsvc.Error{Code: authsvc.CodeAccountLocked}, wantStatus: http.StatusForbidden, wantCode: authsvc.CodeAccountLocked},
		{name: "rate limited", err: &authsvc.Error{Code: authsvc.CodeRateLimited}, wantStatus: http.StatusTooManyRequests, wantCode: authsvc.CodeRateLimited},
		{name: "unauthorized", err: &authsvc.Error{Code: authsvc.CodeUnauthorized}, wantStatus: http.StatusUnauthorized, wantCode: authsvc.CodeUnauthorized},
		{name: "unknown auth code", err: &authsvc.Error{Code: authsvc.ErrorCode("UNKNOWN")}, wantStatus: http.StatusInternalServerError, wantCode: authsvc.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code := mapAuthErrorToHTTP(tt.err)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Fatalf("got (%d,%s), want (%d,%s)", status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestWriteAuthErrorPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	writeAuthError(ctx, http.StatusUnauthorized, authsvc.CodeUnauthorized)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusUnauthorized)
	}

	body := rec.Body.String()
	if !bytes.Contains([]byte(body), []byte(`"success":false`)) {
		t.Fatalf("missing success=false payload: %s", body)
	}
	if !bytes.Contains([]byte(body), []byte(`"error":"UNAUTHORIZED"`)) {
		t.Fatalf("missing error code payload: %s", body)
	}
}

func TestAuthHandler_EarlyValidationBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &AuthHandler{}
	router := gin.New()
	router.POST("/register", h.Register)
	router.POST("/login", h.Login)
	router.POST("/logout", h.Logout)
	router.POST("/refresh", h.Refresh)
	router.GET("/me", h.Me)
	router.POST("/reset-request", h.ResetRequest)
	router.POST("/reset", h.ResetPassword)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		authHeader string
		wantStatus int
		wantCode   string
	}{
		{name: "register invalid json", method: http.MethodPost, path: "/register", body: "{", wantStatus: http.StatusBadRequest, wantCode: string(authsvc.CodeInvalidInput)},
		{name: "login invalid json", method: http.MethodPost, path: "/login", body: "{", wantStatus: http.StatusBadRequest, wantCode: string(authsvc.CodeInvalidInput)},
		{name: "logout missing bearer", method: http.MethodPost, path: "/logout", wantStatus: http.StatusUnauthorized, wantCode: string(authsvc.CodeUnauthorized)},
		{name: "refresh missing bearer", method: http.MethodPost, path: "/refresh", wantStatus: http.StatusUnauthorized, wantCode: string(authsvc.CodeUnauthorized)},
		{name: "me missing bearer", method: http.MethodGet, path: "/me", wantStatus: http.StatusUnauthorized, wantCode: string(authsvc.CodeUnauthorized)},
		{name: "reset-request invalid json", method: http.MethodPost, path: "/reset-request", body: "{", wantStatus: http.StatusBadRequest, wantCode: string(authsvc.CodeInvalidInput)},
		{name: "reset invalid json", method: http.MethodPost, path: "/reset", body: "{", wantStatus: http.StatusBadRequest, wantCode: string(authsvc.CodeInvalidInput)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte(`"error":"`+tt.wantCode+`"`)) {
				t.Fatalf("expected error code %s in body: %s", tt.wantCode, rec.Body.String())
			}
		})
	}
}
