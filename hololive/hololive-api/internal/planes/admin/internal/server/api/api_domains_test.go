package api

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/service/acl"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

func completeTestDeps(t *testing.T) Deps {
	t.Helper()

	alarm := &stubAlarmCRUDForServer{}

	return Deps{
		Logger:     newDiscardLogger(),
		Repository: &member.Repository{},
		Holodex:    &holodexprovider.Service{},
		Alarm:      alarm,
		RoomNames:  alarm,
		ACL:        &acl.Service{},
		Settings:   mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{}),
	}
}

func TestNewDomainHandlersRejectsMissingRequiredDependency(t *testing.T) {
	for _, tc := range []struct {
		name  string
		clear func(*Deps)
	}{
		{"repository", func(d *Deps) { d.Repository = nil }},
		{"holodex", func(d *Deps) { d.Holodex = nil }},
		{"alarm", func(d *Deps) { d.Alarm = nil }},
		{"room names", func(d *Deps) { d.RoomNames = nil }},
		{"acl", func(d *Deps) { d.ACL = nil }},
		{"settings", func(d *Deps) { d.Settings = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := completeTestDeps(t)
			tc.clear(&deps)

			handlers, err := NewDomainHandlers(deps)
			if err == nil || handlers != nil {
				t.Fatalf("NewDomainHandlers() handlers=%v error=%v, want nil handlers and error", handlers, err)
			}
		})
	}
}

// 선택 의존성이 없으면 조립은 성공하고, 해당 endpoint만 문서화된 503을 돌려준다.
func TestNewDomainHandlersOptionalDependenciesRespondUnavailable(t *testing.T) {
	handlers, err := NewDomainHandlers(completeTestDeps(t))
	if err != nil {
		t.Fatalf("NewDomainHandlers() error = %v", err)
	}

	for _, tc := range []struct {
		name    string
		call    func(*DomainHandlers) func(*gin.Context)
		message string
	}{
		{"member cache", func(h *DomainHandlers) func(*gin.Context) { return h.Member.GetMembers }, "member service not available"},
		{"iris rooms", func(h *DomainHandlers) func(*gin.Context) { return h.Room.GetJoinedRooms }, "iris room listing not available"},
		{"dispatch operations", func(h *DomainHandlers) func(*gin.Context) { return h.Alarm.GetDispatchSummary }, "dispatch operations unavailable"},
		{"settings applier", func(h *DomainHandlers) func(*gin.Context) { return h.Settings.GetSettings }, "settings applier not available"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, recorder := newAPITestContext(http.MethodGet, "/test", nil)
			tc.call(handlers)(ctx)

			assertErrorResponse(t, recorder, http.StatusServiceUnavailable, tc.message)
		})
	}
}
