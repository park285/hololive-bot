package workerapp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type stopFunc func()

func (f stopFunc) Stop() { f() }

// runtime 종료는 Holodex 재시도를 infra 정리보다 먼저 멈춰야 한다. 재시도는 요청 ctx 취소와 분리되어
// scheduler Stop 외에는 끝낼 신호가 없고, infra를 먼저 닫으면 재시도가 닫힌 Valkey·PG client를 쓴다.
func TestStopHolodexRetriesBeforeCleanupStopsRetriesFirst(t *testing.T) {
	t.Parallel()

	var calls []string

	cleanup := stopHolodexRetriesBeforeCleanup(
		stopFunc(func() { calls = append(calls, "holodex stop") }),
		func() { calls = append(calls, "infra cleanup") },
	)

	cleanup()

	assert.Equal(t, []string{"holodex stop", "infra cleanup"}, calls)
}

// alarm-worker의 Holodex 서비스는 runtime 설정(HOLODEX_BASE_URL, HOLODEX_API_KEY 등)으로 만들어야 한다.
func TestBuildAlarmHolodexServiceUsesRuntimeHolodexConfig(t *testing.T) {
	t.Parallel()

	type holodexRequest struct{ apiKey, path string }

	seen := make(chan holodexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- holodexRequest{apiKey: r.Header.Get("X-APIKEY"), path: r.URL.Path}:
		default:
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(`{"id":"UC1","name":"probe"}`)); err != nil {
			t.Errorf("write holodex probe response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	appConfig := &settings.Config{
		Holodex: settings.DefaultHolodexOperationalConfig(),
	}

	appConfig.Holodex.BaseURL = server.URL + "/configured"
	appConfig.Holodex.APIKey = "configured-key"
	appConfig.Holodex.DistributedRateLimit.Enabled = false

	holodexService, err := buildAlarmHolodexService(
		appConfig,
		&sharedmodules.InfraModule{Cache: cachemocks.NewLenientClient()},
		nil,
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	t.Cleanup(holodexService.Stop)

	_, err = holodexService.GetChannel(t.Context(), "UC1")
	require.NoError(t, err)

	select {
	case got := <-seen:
		assert.Equal(t, "configured-key", got.apiKey)
		assert.Equal(t, "/configured/channels/UC1", got.path)
	default:
		t.Fatal("holodex probe received no request")
	}
}
