package bootstrap

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

// bot plane의 YouTube rate limiter는 runtime 설정(YOUTUBE_REQUEST_INTERVAL_SECONDS 등)을 따라야 한다.
// 코드 기본값(3초, 분산 제한 on)으로 만들면 이 plane만 운영 설정을 무시한다.
func TestInitScraperHolodexFoundationUsesRuntimeYouTubeConfig(t *testing.T) {
	t.Parallel()

	appConfig := &settings.Config{
		YouTube: settings.DefaultYouTubeOperationalConfig(),
		Holodex: settings.DefaultHolodexOperationalConfig(),
	}

	appConfig.YouTube.RequestInterval = 7 * time.Second
	appConfig.YouTube.DistributedRateLimit.Enabled = false
	appConfig.Holodex.APIKey = "test-key"
	appConfig.Holodex.DistributedRateLimit.Enabled = false

	foundation, err := InitScraperHolodexFoundation(
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

// bot plane의 Holodex 서비스는 runtime 설정(HOLODEX_BASE_URL, HOLODEX_API_KEY 등)으로 만들어야 한다.
func TestInitScraperHolodexFoundationUsesRuntimeHolodexConfig(t *testing.T) {
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

	foundation, err := InitScraperHolodexFoundation(
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
