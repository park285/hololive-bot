package apphttp

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-shared/pkg/contracts/common"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func TestDispatchRoutesRequireAPIKey(t *testing.T) {
	router := gin.New()
	group := router.Group("/api/holo")
	group.Use(middleware.APIKeyAuthMiddleware("dispatch-ops-test-key"))
	registerAlarmRoutes(group, (&api.Handler{}).DomainHandlers().Alarm)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/holo/dispatch/summary"},
		{"GET", "/api/holo/dispatch/deliveries"},
		{"GET", "/api/holo/dispatch/deliveries/1"},
		{"GET", "/api/holo/dispatch/deliveries/1/actions"},
		{"POST", "/api/holo/dispatch/deliveries/1/requeue"},
	} {
		for _, key := range []string{"", "wrong-key", "dispatch-ops-test-key"} {
			request := httptest.NewRequest(route.method, route.path, nil)
			if key != "" {
				request.Header.Set(common.APIKeyHeader, key)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := 401
			if key == "wrong-key" {
				want = 403
			}
			if key == "dispatch-ops-test-key" {
				want = 503
			}
			if response.Code != want {
				t.Fatalf("%s %s authenticated=%v: got %d want %d", route.method, route.path, key == "dispatch-ops-test-key", response.Code, want)
			}
		}
	}
}
