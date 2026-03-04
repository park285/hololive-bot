package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTestGinContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(method, path, http.NoBody)
	return ctx, rec
}

func decodeJSONBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("json unmarshal failed: %v body=%s", err, body)
	}
	return payload
}

func TestSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, rec := newTestGinContext(http.MethodGet, "/success")

	Success(ctx, gin.H{"ok": true, "count": 1})

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusOK)
	}
	payload := decodeJSONBody(t, rec.Body.String())
	if payload["ok"] != true {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

func TestSuccessWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, rec := newTestGinContext(http.MethodPost, "/created")

	SuccessWithStatus(ctx, http.StatusCreated, gin.H{"id": "abc"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusCreated)
	}
	payload := decodeJSONBody(t, rec.Body.String())
	if payload["id"] != "abc" {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

func TestError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, rec := newTestGinContext(http.MethodGet, "/error")

	Error(ctx, http.StatusBadRequest, "invalid input")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusBadRequest)
	}
	payload := decodeJSONBody(t, rec.Body.String())
	if payload["error"] != "invalid input" {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

func TestErrorWithData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, rec := newTestGinContext(http.MethodGet, "/error-with-data")

	ErrorWithData(ctx, http.StatusTooManyRequests, gin.H{"error": "rate limited", "retry_after": 30})

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusTooManyRequests)
	}
	payload := decodeJSONBody(t, rec.Body.String())
	if payload["error"] != "rate limited" {
		t.Fatalf("unexpected payload: %v", payload)
	}
	if payload["retry_after"] != float64(30) {
		t.Fatalf("unexpected payload: %v", payload)
	}
}
