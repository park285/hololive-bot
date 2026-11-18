package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/service/acl"
)

func TestRoomHandlerSetACLRejectsInvalidModeBeforeEnabledMutation(t *testing.T) {
	handler := &RoomHandler{acl: &acl.Service{}, logger: newDiscardLogger()}
	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/acl", []byte(`{"enabled":true,"mode":"not-a-mode"}`))

	handler.SetACL(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "invalid ACL mode") {
		t.Fatalf("body=%s want invalid ACL mode response", rec.Body.String())
	}
}
