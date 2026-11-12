package app

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"
	botruntime2 "github.com/kapu/hololive-api/internal/planes/bot/runtime"
	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func infrastructureConfig(t *testing.T) *apiconfig.RuntimeConfig {
	t.Helper()

	// production provider의 CLIENT TRACKING을 실제로 지원하는 격리된 Valkey만 사용한다.
	address := os.Getenv("TEST_VALKEY_ADDR")
	if address == "" {
		t.Skip("TEST_VALKEY_ADDR must identify an owned test Valkey")
	}

	host, portText, err := net.SplitHostPort(address)
	require.NoError(t, err)
	require.NotEmpty(t, host)

	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	pool := dbtest.NewPool(t)
	pg := pool.Config().ConnConfig
	postgres := settings.PostgresConfig{Host: pg.Host, Port: int(pg.Port), User: pg.User, Password: pg.Password, Database: pg.Database, SSLMode: "disable", PoolMinConns: 0, PoolMaxConns: 2}
	valkey := settings.ValkeyConfig{Host: host, Port: port}

	// infra가 process 하나의 Holodex 서비스를 bot plane 설정으로 만들므로 env 기본값을 그대로 쓴다.
	holodex, err := settings.LoadHolodexConfig()
	require.NoError(t, err)

	// 조립만 확인하므로 외부 Holodex에 닿지 않는 loopback 주소와 시험 키를 쓴다.
	holodex.BaseURL = "http://127.0.0.1:1"
	holodex.APIKey = "test-holodex-key"

	officialSchedule, err := settings.LoadOfficialScheduleRuntimeConfig()
	require.NoError(t, err)

	return &apiconfig.RuntimeConfig{
		Bot: &apiconfig.BotPlaneConfig{
			Postgres: postgres, Valkey: valkey, Kakao: settings.KakaoConfig{ACLEnabled: true, ACLMode: "whitelist"},
			Holodex: holodex, OfficialSchedule: officialSchedule,
		},
		Admin: &apiconfig.AdminPlaneConfig{Postgres: postgres},
	}
}

func TestAPIInfrastructureSharesCacheButSeparatesRuntimePools(t *testing.T) {
	t.Setenv("PGAPPNAME", "hololive-api-test")

	config := infrastructureConfig(t)
	buildCtx, cancel := context.WithCancel(t.Context())
	infra, err := buildAPIInfrastructure(buildCtx, config, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cleanupCancel()

		require.NoError(t, infra.CloseContext(cleanupCtx))
	}()

	cancel()
	require.True(t, infra.bot.Cache.IsConnected(t.Context()))
	require.Same(t, infra.bot.Cache, infra.admin.Cache)
	require.Same(t, infra.bot.MemberCache, infra.admin.MemberCache)
	require.Nil(t, infra.bot.Cleanup)
	require.Nil(t, infra.admin.Cleanup)
	require.Nil(t, infra.bot.StopMemberCache)
	require.Nil(t, infra.admin.StopMemberCache)

	pools := []*pgxpool.Pool{infra.bot.Postgres.GetPool(), infra.admin.Postgres.GetPool()}
	require.NotSame(t, pools[0], pools[1])

	for i, name := range []string{"bot", "admin"} {
		var timeout, application string

		require.NoError(t, pools[i].QueryRow(t.Context(), "SELECT current_setting('statement_timeout'), current_setting('application_name')").Scan(&timeout, &application))
		require.Equal(t, "30s", timeout)
		require.Equal(t, "hololive-api-test/"+name, application)
		require.Equal(t, int32(0), pools[i].Config().MinConns)
		require.Equal(t, time.Second, pools[i].Config().PingTimeout)
	}

	// ACL은 process에 하나만 있고, 관리 변경은 같은 인스턴스의 판정에 바로 보인다.
	require.NotNil(t, infra.acl)
	require.False(t, infra.acl.IsRoomAllowed("3001"))

	added, err := infra.acl.AddRoom(t.Context(), acl.ACLModeWhitelist, "3001")
	require.NoError(t, err)
	require.True(t, added)
	require.True(t, infra.acl.IsRoomAllowed("3001"))

	_, err = infra.admin.MemberCache.LoadAllMembers(t.Context())
	require.NoError(t, err)

	maintenance, cleanup, err := databaseproviders.ProvideMaintenanceDatabaseResources(t.Context(), &config.Bot.Postgres, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	var timeout string

	require.NoError(t, maintenance.Service.GetPool().QueryRow(t.Context(), "SHOW statement_timeout").Scan(&timeout))
	require.Equal(t, "0", timeout)
	cleanup()
	require.NoError(t, infra.CloseContext(t.Context()))
	require.NoError(t, infra.CloseContext(t.Context()))
	require.False(t, infra.bot.Cache.IsConnected(t.Context()))

	for _, pool := range pools {
		require.Error(t, pool.Ping(t.Context()))
	}
}

func TestAPIInfrastructureFailureUnwindsRegistrations(t *testing.T) {
	config := infrastructureConfig(t)
	logger := slog.New(slog.DiscardHandler)

	config.Admin.Postgres.PoolMaxConns = 0

	infra, err := buildAPIInfrastructure(t.Context(), config, logger)
	require.Error(t, err)
	require.Nil(t, infra)

	config.Admin.Postgres.PoolMaxConns = 2
	infra, err = buildAPIInfrastructure(t.Context(), config, logger)
	require.NoError(t, err)
	require.NoError(t, infra.CloseContext(t.Context()))
}

func TestSharedInfrastructureWaitsForBorrowerAfterCallerDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })

	defer finish()

	admin := &adminruntime.AdminAPIRuntime{
		PhotoSync: aggregatePhotoTask(func(ctx context.Context) {
			<-ctx.Done()
			close(entered)
			<-release
		}),
	}
	admin.Start(t.Context(), nil)

	runtime := &Runtime{closeSteps: apiPlaneCloseSteps(apiPlanes{
		admin: admin,
		infra: &apiInfrastructure{closeBot: func() { close(closed) }},
	})}
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	result := make(chan error, 1)

	go func() { result <- runtime.CloseContext(ctx) }()

	<-entered
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)

	select {
	case <-closed:
		t.Fatal("backing resources closed before borrower joined")
	default:
	}

	finish()
	require.ErrorIs(t, runtime.CloseContext(t.Context()), context.Canceled)

	select {
	case <-closed:
	default:
		t.Fatal("backing resources retained after borrower joined")
	}
}

func TestSharedInfrastructureClosesAfterUnstartedBorrowers(t *testing.T) {
	var calls []string

	admin := &adminruntime.AdminAPIRuntime{}
	runtime := &Runtime{closeSteps: apiPlaneCloseSteps(apiPlanes{
		admin: admin,
		infra: &apiInfrastructure{closeBot: func() { calls = append(calls, "infrastructure") }},
	})}
	require.NoError(t, runtime.CloseContext(t.Context()))
	require.NoError(t, runtime.CloseContext(t.Context()))
	require.True(t, admin.ResourceUsersJoined())
	require.Equal(t, []string{"infrastructure"}, calls)
}

func TestPlaneDependenciesBorrowProcessHolodex(t *testing.T) {
	infra := &apiInfrastructure{holodex: &holodexprovider.Service{}, acl: &acl.Service{}}

	bot := botPlaneDependencies(infra)
	admin := adminPlaneDependencies(infra, &botruntime2.BotRuntime{})

	require.Same(t, infra.holodex, bot.Holodex)
	require.Same(t, infra.holodex, admin.Holodex)
	require.Same(t, infra.acl, bot.ACL)
	require.Same(t, infra.acl, admin.ACL)
}

func TestAPIInfrastructureOwnsOneHolodexService(t *testing.T) {
	infra, err := buildAPIInfrastructure(t.Context(), infrastructureConfig(t), slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, infra.CloseContext(context.WithoutCancel(t.Context()))) })

	require.NotNil(t, infra.holodex)
}

// process 하나의 Holodex 서비스는 bot plane 설정(HOLODEX_BASE_URL, HOLODEX_API_KEY 등)으로 만들어야 한다.
func TestBuildHolodexServiceUsesBotPlaneHolodexConfig(t *testing.T) {
	t.Parallel()

	type holodexRequest struct{ apiKey, path string }

	seen := make(chan holodexRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- holodexRequest{apiKey: r.Header.Get("X-APIKEY"), path: r.URL.Path}:
		default:
		}

		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(`[]`)); err != nil {
			t.Errorf("write holodex probe response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	botConfig := &apiconfig.BotPlaneConfig{Holodex: settings.DefaultHolodexOperationalConfig()}

	botConfig.Holodex.BaseURL = server.URL + "/configured"
	botConfig.Holodex.APIKey = "configured-key"
	botConfig.Holodex.DistributedRateLimit.Enabled = false

	holodex, err := buildHolodexService(t.Context(), botConfig, newHolodexTestMemberCache(t), cachemocks.NewLenientClient(), slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(holodex.Stop)

	_, err = holodex.GetChannelSchedule(t.Context(), "UC1", 24, false)
	require.NoError(t, err)

	select {
	case got := <-seen:
		assert.Equal(t, "configured-key", got.apiKey)
		assert.Equal(t, "/configured/live", got.path)
	default:
		t.Fatal("holodex probe received no request")
	}
}

// newHolodexTestMemberCache는 migration 시드 DB를 읽는 member cache다. 공식 일정 식별 색인은 멤버 적재에 실패하면
// 기동 오류이므로 Holodex 서비스 시험도 실제 멤버 원천을 준다.
func newHolodexTestMemberCache(t *testing.T) *member.Cache {
	t.Helper()

	pool := dbtest.NewPool(t)
	repository := member.NewMemberRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }})

	memberCache, err := member.NewMemberCache(t.Context(), repository, sharedtestutil.NewTestCacheService(t.Context(), t), slog.New(slog.DiscardHandler), member.CacheConfig{})
	require.NoError(t, err)
	t.Cleanup(memberCache.Close)

	return memberCache
}
