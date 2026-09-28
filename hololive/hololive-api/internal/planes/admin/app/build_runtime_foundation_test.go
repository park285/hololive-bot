package app

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

// admin plane의 YouTube rate limiter는 runtime 설정(YOUTUBE_REQUEST_INTERVAL_SECONDS 등)을 따라야 한다.
func TestBuildScraperHolodexFoundationUsesRuntimeYouTubeConfig(t *testing.T) {
	t.Parallel()

	appConfig := &settings.Config{
		YouTube: settings.DefaultYouTubeOperationalConfig(),
		Holodex: settings.DefaultHolodexOperationalConfig(),
	}

	appConfig.YouTube.RequestInterval = 7 * time.Second
	appConfig.YouTube.DistributedRateLimit.Enabled = false
	appConfig.Holodex.APIKey = testAPIKey
	appConfig.Holodex.DistributedRateLimit.Enabled = false

	foundation, err := buildScraperHolodexFoundation(
		t.Context(),
		appConfig,
		&sharedmodules.InfraModule{Cache: cachemocks.NewLenientClient(), MemberCache: newFoundationTestMemberCache(t)},
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	t.Cleanup(foundation.HolodexService.Stop)

	first, err := foundation.SharedRL.TryReserve(t.Context())
	require.NoError(t, err)
	require.True(t, first.Allowed)

	second, err := foundation.SharedRL.TryReserve(t.Context())
	require.NoError(t, err)
	assert.False(t, second.Allowed)
	assert.InDelta(t, float64(7*time.Second), float64(second.RetryAfter), float64(time.Second),
		"rate limiter interval must come from appConfig.YouTube.RequestInterval")
}

// alarm provider URL이 없으면 in-process AlarmService로 대신하지 않고 기동을 실패시킨다.
func TestBuildAlarmModeComponentsRequiresAlarmProviderURL(t *testing.T) {
	t.Parallel()

	components, err := buildAlarmModeComponents(&settings.Config{}, nil, slog.New(slog.DiscardHandler))

	require.Nil(t, components)
	require.EqualError(t, err, "alarm provider URL (ALARM_INTERNAL_URL) is required")
}

// admin plane의 Holodex 서비스는 runtime 설정(HOLODEX_BASE_URL, HOLODEX_API_KEY 등)으로 만들어야 한다.
func TestBuildScraperHolodexFoundationUsesRuntimeHolodexConfig(t *testing.T) {
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
		YouTube: settings.DefaultYouTubeOperationalConfig(),
		Holodex: settings.DefaultHolodexOperationalConfig(),
	}

	appConfig.YouTube.DistributedRateLimit.Enabled = false
	appConfig.Holodex.BaseURL = server.URL + "/configured"
	appConfig.Holodex.APIKey = "configured-key"
	appConfig.Holodex.DistributedRateLimit.Enabled = false

	foundation, err := buildScraperHolodexFoundation(
		t.Context(),
		appConfig,
		&sharedmodules.InfraModule{Cache: cachemocks.NewLenientClient(), MemberCache: newFoundationTestMemberCache(t)},
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	t.Cleanup(foundation.HolodexService.Stop)

	_, err = foundation.HolodexService.GetChannel(t.Context(), "UC1")
	require.NoError(t, err)

	select {
	case got := <-seen:
		assert.Equal(t, "configured-key", got.apiKey)
		assert.Equal(t, "/configured/channels/UC1", got.path)
	default:
		t.Fatal("holodex probe received no request")
	}
}

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

// newFoundationTestMemberCache는 migration 시드 DB를 읽는 member cache다. 공식 일정 식별 색인은 멤버 적재에 실패하면
// 기동 오류이므로(DEC-20260926-hololive-source-fallbacks-retirement) foundation 테스트도 실제 멤버 원천을 준다.
func newFoundationTestMemberCache(t *testing.T) *member.Cache {
	t.Helper()

	pool := dbtest.NewPool(t)
	repository := member.NewMemberRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, slog.New(slog.DiscardHandler))

	memberCache, err := member.NewMemberCache(t.Context(), repository, nil, slog.New(slog.DiscardHandler), member.CacheConfig{})
	require.NoError(t, err)

	return memberCache
}
