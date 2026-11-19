package api

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/service/acl"
)

func TestServerHandlers_InvalidJSONResponseIsSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.DiscardHandler)
	room := &RoomHandler{logger: logger, acl: &acl.Service{}}

	testCases := []struct {
		name   string
		path   string
		params gin.Params
		call   gin.HandlerFunc
	}{
		{"alarm delete", "/api/holo/alarm/delete", nil, (&AlarmHandler{logger: logger}).DeleteAlarm},
		{"room add", "/api/holo/rooms/add", nil, room.AddRoom},
		{"room remove", "/api/holo/rooms/remove", nil, room.RemoveRoom},
		{"room acl", "/api/holo/rooms/acl", nil, room.SetACL},
		{"member add", "/api/holo/members", nil, newMemberHandler(Deps{Logger: logger}).AddMember},
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
