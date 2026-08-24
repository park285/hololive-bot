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

package api

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/service/acl"
)

func TestServerHandlers_DoNotUseErrError(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("failed to read server package directory: %v", err)
	}

	var offenders []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}

		cleanName := filepath.Clean(name)
		if filepath.Dir(cleanName) != "." || cleanName != name {
			t.Fatalf("unexpected file path outside package directory: %s", name)
		}

		content, readErr := os.ReadFile(cleanName)
		if readErr != nil {
			t.Fatalf("failed to read %s: %v", name, readErr)
		}

		if strings.Contains(string(content), "err.Error()") {
			offenders = append(offenders, name)
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("raw err.Error() usage found in server handlers: %v", offenders)
	}
}

func TestServerHandlers_InvalidJSONResponseIsSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.DiscardHandler)
	base := &Handler{
		logger: logger,
		acl:    &acl.Service{},
	}

	noticeParams := gin.Params{{Key: templateKeyParam, Value: "notice"}}

	testCases := []struct {
		name   string
		path   string
		params gin.Params
		call   gin.HandlerFunc
	}{
		{"alarm delete", "/api/holo/alarm/delete", nil, (&AlarmHandler{Handler: base}).DeleteAlarm},
		{"room add", "/api/holo/rooms/add", nil, (&RoomHandler{Handler: base}).AddRoom},
		{"room remove", "/api/holo/rooms/remove", nil, (&RoomHandler{Handler: base}).RemoveRoom},
		{"room acl", "/api/holo/rooms/acl", nil, (&RoomHandler{Handler: base}).SetACL},
		{"member add", "/api/holo/members", nil, (&MemberHandler{Handler: base}).AddMember},
		{"template upsert", "/api/holo/templates/notice", noticeParams, (&TemplateHandler{Handler: base}).UpsertTemplate},
		{"template preview", "/api/holo/templates/notice/preview", noticeParams, (&TemplateHandler{Handler: base}).PreviewTemplate},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, rec := newMalformedJSONContext(http.MethodPost, tc.path, tc.params)
			tc.call(ctx)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			var payload map[string]any

			if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			if payload["error"] != "invalid request body" {
				t.Fatalf("error response = %v, want %q", payload["error"], "invalid request body")
			}

			body := rec.Body.String()
			if strings.Contains(body, "invalid character") || strings.Contains(body, "EOF") {
				t.Fatalf("response leaked parser details: %s", body)
			}
		})
	}
}

func newMalformedJSONContext(method, path string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	req := httptest.NewRequestWithContext(context.Background(), method, path, bytes.NewBufferString(`{"`))
	req.Header.Set("Content-Type", "application/json")

	ctx.Request = req
	ctx.Params = params

	return ctx, rec
}
