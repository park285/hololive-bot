package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
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

		content, readErr := os.ReadFile(name)
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := &APIHandler{
		logger: logger,
		acl:    &acl.Service{},
	}

	testCases := []struct {
		name   string
		path   string
		params gin.Params
		call   func(*gin.Context)
	}{
		{
			name: "alarm delete",
			path: "/api/holo/alarm/delete",
			call: (&AlarmAPIHandler{APIHandler: base}).DeleteAlarm,
		},
		{
			name: "room add",
			path: "/api/holo/rooms/add",
			call: (&RoomAPIHandler{APIHandler: base}).AddRoom,
		},
		{
			name: "room remove",
			path: "/api/holo/rooms/remove",
			call: (&RoomAPIHandler{APIHandler: base}).RemoveRoom,
		},
		{
			name: "room acl",
			path: "/api/holo/rooms/acl",
			call: (&RoomAPIHandler{APIHandler: base}).SetACL,
		},
		{
			name: "member add",
			path: "/api/holo/members",
			call: (&MemberAPIHandler{APIHandler: base}).AddMember,
		},
		{
			name: "template upsert",
			path: "/api/holo/templates/notice",
			params: gin.Params{
				{Key: "key", Value: "notice"},
			},
			call: (&TemplateAPIHandler{APIHandler: base}).UpsertTemplate,
		},
		{
			name: "template preview",
			path: "/api/holo/templates/notice/preview",
			params: gin.Params{
				{Key: "key", Value: "notice"},
			},
			call: (&TemplateAPIHandler{APIHandler: base}).PreviewTemplate,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, rec := newMalformedJSONContext(http.MethodPost, tc.path, tc.params)
			tc.call(ctx)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
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

	req := httptest.NewRequest(method, path, bytes.NewBufferString(`{"`))
	req.Header.Set("Content-Type", "application/json")

	ctx.Request = req
	ctx.Params = params

	return ctx, rec
}
