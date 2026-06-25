package alarm

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInternalRouteRegistrarRegistersCompleteAlarmRouteSet(t *testing.T) {
	router := gin.New()
	registrar := NewInternalRouteRegistrar("secret", fakeAlarmCRUD{}, slog.New(slog.DiscardHandler))

	require.NoError(t, registrar(router))

	expected := []string{
		"POST " + contractsalarm.BasePath + contractsalarm.AddRoute,
		"POST " + contractsalarm.BasePath + contractsalarm.RemoveRoute,
		"GET " + contractsalarm.BasePath + contractsalarm.RoomRoute,
		"GET " + contractsalarm.BasePath + contractsalarm.RoomViewRoute,
		"POST " + contractsalarm.BasePath + contractsalarm.ClearRoute,
		"GET " + contractsalarm.BasePath + contractsalarm.NextStreamRoute,
		"PUT " + contractsalarm.BasePath + contractsalarm.SettingsRoute,
		"PUT " + contractsalarm.BasePath + contractsalarm.RoomNameRoute,
		"PUT " + contractsalarm.BasePath + contractsalarm.UserNameRoute,
		"GET " + contractsalarm.BasePath + contractsalarm.KeysRoute,
	}
	assert.ElementsMatch(t, expected, routeKeys(router.Routes()))
}

func TestNewInternalRouteRegistrarAppliesAPIKeyAuth(t *testing.T) {
	router := gin.New()
	registrar := NewInternalRouteRegistrar("secret", fakeAlarmCRUD{}, slog.New(slog.DiscardHandler))
	require.NoError(t, registrar(router))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, contractsalarm.BasePath+contractsalarm.KeysRoute, http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestNewInternalRouteRegistrarSkipsWhenRouterOrAlarmCRUDMissing(t *testing.T) {
	require.NoError(t, NewInternalRouteRegistrar("secret", nil, slog.New(slog.DiscardHandler))(gin.New()))
	require.NoError(t, NewInternalRouteRegistrar("secret", fakeAlarmCRUD{}, slog.New(slog.DiscardHandler))(nil))
}

func routeKeys(routes gin.RoutesInfo) []string {
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

type fakeAlarmCRUD struct{}

func (fakeAlarmCRUD) AddAlarm(context.Context, *domain.AddAlarmRequest) (bool, error) {
	return false, nil
}

func (fakeAlarmCRUD) RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error) {
	return false, nil
}

func (fakeAlarmCRUD) GetRoomAlarms(context.Context, string) ([]string, error) {
	return nil, nil
}

func (fakeAlarmCRUD) GetRoomAlarmsWithTypes(context.Context, string) ([]*domain.Alarm, error) {
	return nil, nil
}

func (fakeAlarmCRUD) ListRoomAlarmsView(context.Context, string) ([]domain.AlarmListView, error) {
	return nil, nil
}

func (fakeAlarmCRUD) ClearRoomAlarms(context.Context, string) (int, error) {
	return 0, nil
}

func (fakeAlarmCRUD) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return nil, nil
}

func (fakeAlarmCRUD) WarmCacheFromDB(context.Context) error {
	return nil
}

func (fakeAlarmCRUD) SetRoomName(context.Context, string, string) error {
	return nil
}

func (fakeAlarmCRUD) SetUserName(context.Context, string, string) error {
	return nil
}

func (fakeAlarmCRUD) GetNextStreamInfo(context.Context, string) (*domain.NextStreamInfo, error) {
	return nil, nil
}

func (fakeAlarmCRUD) UpdateAlarmAdvanceMinutes(context.Context, int) []int {
	return nil
}

func (fakeAlarmCRUD) GetTargetMinutes() []int {
	return nil
}
