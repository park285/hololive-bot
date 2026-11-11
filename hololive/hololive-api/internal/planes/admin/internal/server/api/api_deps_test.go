package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/service/member"
)

func TestSafeLogger_ReturnsDefaultOnNil(t *testing.T) {
	var h *MemberHandler

	logger := h.safeLogger()

	if logger == nil {
		t.Fatal("safeLogger returned nil on nil receiver")
	}
}

func TestSafeLogger_ReturnsConfiguredLogger(t *testing.T) {
	want := newDiscardLogger()
	h := &MemberHandler{logger: want}
	got := h.safeLogger()

	if got != want {
		t.Fatal("safeLogger did not return configured logger")
	}
}

func TestLogActivity_NilSafety(_ *testing.T) {
	var h *AlarmHandler

	h.logActivity("test", "summary", nil)

	h2 := &AlarmHandler{}
	h2.logActivity("test", "summary", nil)
}

func TestRespondServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, rec := newAPITestContext(http.MethodGet, "/test", nil)
	respondServiceUnavailable(ctx, "service down")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusServiceUnavailable)
	}

	assertErrorResponse(t, rec, http.StatusServiceUnavailable, "service down")
}

func TestRequireMemberDeps(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil handler", func(t *testing.T) {
		var h *MemberHandler

		ctx, rec := newAPITestContext(http.MethodGet, "/test", nil)
		ok := h.requireMemberDeps(ctx)

		if ok {
			t.Fatal("requireMemberDeps returned true on nil handler")
		}

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("missing member cache", func(t *testing.T) {
		h := newMemberHandler(Deps{Repository: &member.Repository{}})
		ctx, rec := newAPITestContext(http.MethodGet, "/test", nil)
		ok := h.requireMemberDeps(ctx)

		if ok {
			t.Fatal("requireMemberDeps returned true with nil memberCache")
		}

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}
