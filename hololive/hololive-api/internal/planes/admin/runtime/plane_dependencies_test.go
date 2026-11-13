package adminruntime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v5/iris"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

var errUnusedAlarmProvider = errors.New("alarm provider is not used by this test")

// unusedAlarmProvider는 alarm-worker를 부르지 않는 경로 시험에서 필수 alarm client 자리를 채운다.
type unusedAlarmProvider struct{}

func (unusedAlarmProvider) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return nil, errUnusedAlarmProvider
}

func (unusedAlarmProvider) CountAlarmEntries(context.Context) (int, error) {
	return 0, errUnusedAlarmProvider
}

func (unusedAlarmProvider) RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error) {
	return false, errUnusedAlarmProvider
}

func (unusedAlarmProvider) SetRoomName(context.Context, string, string) error {
	return errUnusedAlarmProvider
}

func (unusedAlarmProvider) UpdateAlarmAdvanceMinutes(context.Context, int) (domain.AdvanceMinutesResult, error) {
	return domain.AdvanceMinutesResult{}, errUnusedAlarmProvider
}

func (unusedAlarmProvider) GetTargetMinutes() []int { return nil }

func (unusedAlarmProvider) Close() error { return nil }

// buildTestAdminHandlers는 process 조립과 같은 buildAdminHandlers 경로로 handler를 만든다. 이 시험 경로는
// alarm-worker와 Holodex를 호출하지 않으므로 둘은 자리 채움이다.
type onceCollector struct {
	requested atomic.Bool
}

func (c *onceCollector) RequestCollect() bool {
	return c.requested.CompareAndSwap(false, true)
}

func buildTestAdminHandlers(t *testing.T, infra *sharedmodules.InfraModule, deps Dependencies, logger *slog.Logger) *server.DomainHandlers {
	t.Helper()

	settingsService, err := adminsettings.BuildSettingsService(filepath.Join(t.TempDir(), "settings.json"), nil, logger)
	if err != nil {
		t.Fatalf("BuildSettingsService() error = %v", err)
	}

	if deps.Holodex == nil {
		deps.Holodex = &holodexprovider.Service{}
	}

	var schedulePosters *scheduleposter.Store

	if infra.Postgres != nil && infra.Postgres.GetPool() != nil {
		schedulePosters, err = scheduleposter.NewStore(infra.Postgres.GetPool())
		if err != nil {
			t.Fatal(err)
		}
	}

	domains, err := buildAdminHandlers(infra, deps, unusedAlarmProvider{}, adminAPISettings{
		service: settingsService,
		applier: adminsettings.NewSettingsApplier(unusedAlarmProvider{}),
	}, nil, schedulePosters, &onceCollector{}, logger)
	if err != nil {
		t.Fatalf("buildAdminHandlers() error = %v", err)
	}

	return domains
}

// recordingIrisRooms는 bot plane이 넘기는 Iris client 방 목록 조회 자리에 들어가 호출 횟수를 센다.
type recordingIrisRooms struct{ calls atomic.Int32 }

func (r *recordingIrisRooms) GetRooms(context.Context) (*iris.RoomListResponse, error) {
	r.calls.Add(1)

	return &iris.RoomListResponse{Rooms: []iris.RoomSummary{{ChatID: 3001}}}, nil
}

func TestAdminRoutesUseInjectedPlaneDependencies(t *testing.T) {
	rooms := &recordingIrisRooms{}
	serve, aclService, _ := newAdminTestRouter(t, rooms)

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

// newAdminTestRouter는 process 조립과 같은 buildAdminHandlers·ProvideAPIRouter 경로로 admin router를 만든다.
// ACL Service는 시험 DB를 쓰는 실제 인스턴스다.
func newAdminTestRouter(t *testing.T, rooms *recordingIrisRooms) (adminTestServe, *acl.Service, *pgxpool.Pool) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	aclService, err := acl.NewACLService(t.Context(), postgres, logger, true, acl.ACLModeWhitelist, nil)
	if err != nil {
		t.Fatal(err)
	}

	domains := buildTestAdminHandlers(t, &sharedmodules.InfraModule{Postgres: postgres, MemberRepository: member.NewMemberRepository(postgres)},
		Dependencies{ACL: aclService, IrisRooms: rooms}, logger)

	appConfig := &apiconfig.AdminPlaneConfig{Server: settings.ServerConfig{APIKey: testAPIKey}}

	router, err := apphttp.ProvideAPIRouter(t.Context(), appConfig, logger, domains, sharedtestutil.NewTestCacheService(t.Context(), t), sharedreadiness.NewProbe("admin"))
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

	return serve, aclService, pool
}
