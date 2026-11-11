package adminruntime

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v5/iris"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"
	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

type recordingIrisRooms struct{ calls atomic.Int32 }

func (r *recordingIrisRooms) GetRooms(context.Context) (*iris.RoomListResponse, error) {
	r.calls.Add(1)

	return &iris.RoomListResponse{Rooms: []iris.RoomSummary{{ChatID: 3001}}}, nil
}

func TestAdminRoutesUseInjectedPlaneDependencies(t *testing.T) {
	rooms := &recordingIrisRooms{}
	serve, aclService := newAdminTestRouter(t, rooms)

	if rec := serve(http.MethodGet, "/api/holo/rooms/joined", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "3001") || rooms.calls.Load() != 1 {
		t.Fatalf("joined rooms = %d %s, iris calls %d; want 200 from injected lister", rec.Code, rec.Body.String(), rooms.calls.Load())
	}

	if aclService.IsRoomAllowed("3001") {
		t.Fatal("room allowed before admin add")
	}

	if rec := serve(http.MethodPost, "/api/holo/rooms", `{"room":"3001","mode":"whitelist"}`); rec.Code != http.StatusOK {
		t.Fatalf("add room = %d %s, want 200", rec.Code, rec.Body.String())
	}

	if !aclService.IsRoomAllowed("3001") {
		t.Fatal("admin add did not reach the shared ACL instance")
	}
}

type adminTestServe func(method, path, body string) *httptest.ResponseRecorder

func newAdminTestRouter(t *testing.T, rooms *recordingIrisRooms) (adminTestServe, *acl.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	aclService, err := acl.NewACLService(t.Context(), postgres, logger, true, acl.ACLModeWhitelist, nil)
	if err != nil {
		t.Fatal(err)
	}

	handler := buildAdminHandler(&sharedmodules.InfraModule{}, &apifoundation.ScraperHolodexFoundation{}, &alarmModeComponents{},
		aclService, rooms, nil, nil, logger)

	appConfig := &apiconfig.AdminPlaneConfig{Server: settings.ServerConfig{APIKey: testAPIKey}}

	router, err := apphttp.ProvideAPIRouter(t.Context(), appConfig, logger, handler.DomainHandlers(), sharedtestutil.NewTestCacheService(t.Context(), t))
	if err != nil {
		t.Fatal(err)
	}

	serve := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()

		req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		req.Header.Set("X-API-Key", testAPIKey)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return rec
	}

	return serve, aclService
}
