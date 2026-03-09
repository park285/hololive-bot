package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/auth"
)

func TestCSRFProtection_AllowsValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "test-secret"
	sessionID := "session-123"
	token := NewCSRFToken(sessionID, secret)
	if token == "" {
		t.Fatalf("expected non-empty csrf token")
	}

	r := gin.New()
	r.Use(CSRFProtection(secret))
	r.POST("/protected", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: auth.SignSessionID(sessionID, secret)})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})
	req.Header.Set("X-CSRF-Token", token)

	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}
}

func TestCSRFProtection_RejectsMissingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "test-secret"
	sessionID := "session-123"
	token := NewCSRFToken(sessionID, secret)

	r := gin.New()
	r.Use(CSRFProtection(secret))
	r.POST("/protected", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: auth.SignSessionID(sessionID, secret)})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: token})

	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}
}

func TestCSRFProtection_RejectsTokenMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "test-secret"
	sessionID := "session-123"
	tokenCookie := NewCSRFToken(sessionID, secret)
	tokenHeader := NewCSRFToken(sessionID, secret)

	r := gin.New()
	r.Use(CSRFProtection(secret))
	r.POST("/protected", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: auth.SignSessionID(sessionID, secret)})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: tokenCookie})
	req.Header.Set("X-CSRF-Token", tokenHeader)

	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}
}

func TestCSRFProtection_RejectsForgedSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "test-secret"
	sessionID := "session-123"
	token := NewCSRFToken(sessionID, secret)
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("expected token with two parts")
	}
	forged := parts[0] + "." + "forged"

	r := gin.New()
	r.Use(CSRFProtection(secret))
	r.POST("/protected", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: auth.SignSessionID(sessionID, secret)})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: forged})
	req.Header.Set("X-CSRF-Token", forged)

	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}
}

func TestCSRFProtection_SkipsGetRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secret := "test-secret"

	r := gin.New()
	r.Use(CSRFProtection(secret))
	r.GET("/protected", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}
}
