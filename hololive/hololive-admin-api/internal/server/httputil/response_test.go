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

package httputil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	json "github.com/park285/shared-go/pkg/json"
)

func newTestGinContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	ctx.Request = httptest.NewRequestWithContext(context.Background(), method, path, http.NoBody)

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
