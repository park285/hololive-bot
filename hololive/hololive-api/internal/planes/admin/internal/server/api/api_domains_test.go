package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDomainHandlers_MissingDependenciesRejectRequests(t *testing.T) {
	var base *Handler

	domains := base.DomainHandlers()

	for _, tc := range []struct {
		name string
		call gin.HandlerFunc
	}{
		{"member", domains.Member.GetMembers},
		{"alarm", domains.Alarm.GetAlarms},
		{"room", domains.Room.GetRooms},
		{"stats", domains.Stats.GetStats},
		{"settings", domains.Settings.GetSettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, recorder := newAPITestContext(http.MethodGet, "/test", nil)
			tc.call(ctx)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
