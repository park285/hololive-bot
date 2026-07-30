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

package httpserver

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	json "github.com/park285/shared-go/pkg/json"
)

func TestRespondError(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/error", func(c *gin.Context) {
		RespondError(c, http.StatusBadRequest, "invalid input", gin.H{
			"code": "invalid_input",
		})
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/error", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := payload["error"]; got != "invalid input" {
		t.Fatalf("error = %v, want %q", got, "invalid input")
	}
	if got := payload["code"]; got != "invalid_input" {
		t.Fatalf("code = %v, want %q", got, "invalid_input")
	}
}

func TestRespondInternalError(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	router := gin.New()
	router.GET("/internal", func(c *gin.Context) {
		RespondInternalError(
			logger,
			c,
			"internal error",
			"failed to query db",
			errors.New("db timeout"),
			slog.String("route", "/internal"),
		)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/internal", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := payload["error"]; got != "internal error" {
		t.Fatalf("error = %v, want %q", got, "internal error")
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "failed to query db") {
		t.Fatalf("log does not contain message: %s", logOutput)
	}
	if !strings.Contains(logOutput, "db timeout") {
		t.Fatalf("log does not contain error cause: %s", logOutput)
	}
}
