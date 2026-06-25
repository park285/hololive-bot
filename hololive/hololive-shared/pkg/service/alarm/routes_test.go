package alarm

import (
	"context"
	"log/slog"
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

	routes := routeSet(router.Routes())
	assert.Contains(t, routes, "POST "+contractsalarm.BasePath+contractsalarm.AddRoute)
	assert.Contains(t, routes, "POST "+contractsalarm.BasePath+contractsalarm.RemoveRoute)
	assert.Contains(t, routes, "GET "+contractsalarm.BasePath+contractsalarm.RoomRoute)
	assert.Contains(t, routes, "GET "+contractsalarm.BasePath+contractsalarm.RoomViewRoute)
	assert.Contains(t, routes, "POST "+contractsalarm.BasePath+contractsalarm.ClearRoute)
	assert.Contains(t, routes, "GET "+contractsalarm.BasePath+contractsalarm.NextStreamRoute)
	assert.Contains(t, routes, "PUT "+contractsalarm.BasePath+contractsalarm.SettingsRoute)
	assert.Contains(t, routes, "PUT "+contractsalarm.BasePath+contractsalarm.RoomNameRoute)
	assert.Contains(t, routes, "PUT "+contractsalarm.BasePath+contractsalarm.UserNameRoute)
	assert.Contains(t, routes, "GET "+contractsalarm.BasePath+contractsalarm.KeysRoute)
}

func TestNewInternalRouteRegistrarSkipsWhenRouterOrAlarmCRUDMissing(t *testing.T) {
	require.NoError(t, NewInternalRouteRegistrar("secret", nil, slog.New(slog.DiscardHandler))(gin.New()))
	require.NoError(t, NewInternalRouteRegistrar("secret", fakeAlarmCRUD{}, slog.New(slog.DiscardHandler))(nil))
}

func routeSet(routes gin.RoutesInfo) map[string]struct{} {
	out := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		out[route.Method+" "+route.Path] = struct{}{}
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
