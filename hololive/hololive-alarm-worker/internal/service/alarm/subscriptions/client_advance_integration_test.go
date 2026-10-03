package subscriptions_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/subscriptions"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newAdvanceService(t *testing.T) *subscriptions.AlarmService {
	t.Helper()

	service, err := subscriptions.NewAlarmService(cachemocks.NewLenientClient(), nil, &alarm.Repository{}, slog.New(slog.DiscardHandler), []int{5, 3, 1})
	require.NoError(t, err)

	return service
}

func newAdvanceRouter(service *subscriptions.AlarmService, apiKey string) http.Handler {
	router := gin.New()
	group := router.Group("/")

	if apiKey != "" {
		group.Use(middleware.APIKeyAuthMiddleware(apiKey))
	}

	alarm.NewHandler(service, slog.New(slog.DiscardHandler)).RegisterInternalRoutes(group)

	return router
}

func TestClientAdvanceUsesRealWorkerAndConfirmedRejections(t *testing.T) {
	t.Parallel()

	service := newAdvanceService(t)
	server := httptest.NewServer(newAdvanceRouter(service, "test-key"))
	t.Cleanup(server.Close)

	client := alarm.NewClientWithAPIKey(server.URL, "test-key", nil)

	result, err := client.UpdateAlarmAdvanceMinutes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, domain.ApplyConfirmed, result.Outcome)
	assert.Equal(t, []int{10, 3, 1}, result.TargetMinutes)
	assert.Equal(t, result.TargetMinutes, service.GetTargetMinutes())

	result.TargetMinutes[0] = 999

	assert.Equal(t, []int{10, 3, 1}, client.GetTargetMinutes())
	assert.Equal(t, []int{10, 3, 1}, service.GetTargetMinutes())

	for _, tc := range []struct {
		name    string
		key     string
		minutes int
		status  int
		code    string
	}{
		{name: "input rejection", key: "test-key", status: http.StatusBadRequest, code: "invalid_request_body"},
		{name: "missing authentication", minutes: 10, status: http.StatusUnauthorized, code: "unauthorized"},
		{name: "invalid authentication", key: "wrong-key", minutes: 10, status: http.StatusForbidden, code: "forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := alarm.NewClientWithAPIKey(server.URL, tc.key, nil).UpdateAlarmAdvanceMinutes(t.Context(), tc.minutes)
			require.Error(t, err)
			assert.Equal(t, domain.ApplyRejected, result.Outcome)
			assert.Empty(t, result.TargetMinutes)

			apiErr, ok := errors.AsType[*httputil.APIError](err)
			require.True(t, ok)
			assert.Equal(t, tc.status, apiErr.StatusCode)
			assert.Equal(t, tc.code, apiErr.Code)
			assert.Equal(t, []int{10, 3, 1}, service.GetTargetMinutes())
		})
	}
}

func TestClientAdvanceAppliedThenResponseLostIsUnknown(t *testing.T) {
	t.Parallel()

	service := newAdvanceService(t)
	worker := newAdvanceRouter(service, "")

	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		// 실제 worker를 실행해 target을 적용한 뒤 응답을 전송하지 않고 연결을 끊는다.
		rec := httptest.NewRecorder()
		worker.ServeHTTP(rec, r)

		if rec.Code != http.StatusOK {
			t.Errorf("worker status = %d, want 200", rec.Code)
		}

		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack response: %v", err)

			return
		}

		if err := conn.Close(); err != nil {
			t.Errorf("close response connection: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := alarm.NewClient(server.URL, nil)

	result, err := client.UpdateAlarmAdvanceMinutes(t.Context(), 12)
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, domain.ApplyUnknown, result.Outcome)
	assert.Equal(t, 12, result.RequestedMinutes)
	assert.Empty(t, result.TargetMinutes)
	assert.Empty(t, client.GetTargetMinutes())
	assert.Equal(t, []int{12, 3, 1}, service.GetTargetMinutes())
	assert.Equal(t, int32(1), calls.Load())
}
