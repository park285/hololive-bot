package app

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"
	"github.com/kapu/hololive-api/internal/service/acl"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
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

	return &apiconfig.RuntimeConfig{
		Bot:   &apiconfig.BotPlaneConfig{Postgres: postgres, Valkey: valkey, Kakao: settings.KakaoConfig{ACLEnabled: true, ACLMode: "whitelist"}},
		Admin: &apiconfig.AdminPlaneConfig{Postgres: postgres, Valkey: valkey},
		LLM:   &apiconfig.LLMSchedulerConfig{Postgres: postgres, Valkey: valkey},
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
	require.Same(t, infra.bot.Cache, infra.llm.Cache)
	require.Same(t, infra.bot.MemberCache, infra.admin.MemberCache)
	require.Same(t, infra.bot.MemberCache, infra.llm.MemberCache)
	require.Nil(t, infra.bot.Cleanup)
	require.Nil(t, infra.admin.Cleanup)
	require.Nil(t, infra.llm.Cleanup)
	require.Nil(t, infra.bot.StopMemberCache)
	require.Nil(t, infra.admin.StopMemberCache)
	require.Nil(t, infra.llm.StopMemberCache)

	pools := []*pgxpool.Pool{infra.bot.Postgres.GetPool(), infra.admin.Postgres.GetPool(), infra.llm.Postgres.GetPool()}
	require.NotSame(t, pools[0], pools[1])
	require.NotSame(t, pools[0], pools[2])

	for i, name := range []string{"bot", "admin", "llm_scheduler"} {
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

	_, err = infra.llm.MemberCache.LoadAllMembers(t.Context())
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

	for _, failAdmin := range []bool{true, false} {
		if failAdmin {
			config.Admin.Postgres.PoolMaxConns = 0
		} else {
			config.LLM.Postgres.PoolMaxConns = 0
		}

		infra, err := buildAPIInfrastructure(t.Context(), config, logger)
		require.Error(t, err)
		require.Nil(t, infra)

		config.Admin.Postgres.PoolMaxConns = 2
		config.LLM.Postgres.PoolMaxConns = 2
		infra, err = buildAPIInfrastructure(t.Context(), config, logger)
		require.NoError(t, err)
		require.NoError(t, infra.CloseContext(t.Context()))
	}
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
		infra: &apiInfrastructure{closeResources: func() { close(closed) }},
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

	admin := &adminruntime.AdminAPIRuntime{
		Managed: lifecycle.NewManaged(func() { calls = append(calls, "admin") }),
	}
	runtime := &Runtime{closeSteps: apiPlaneCloseSteps(apiPlanes{
		admin: admin,
		infra: &apiInfrastructure{closeResources: func() { calls = append(calls, "infrastructure") }},
	})}
	require.NoError(t, runtime.CloseContext(t.Context()))
	require.NoError(t, runtime.CloseContext(t.Context()))
	require.True(t, admin.ResourceUsersJoined())
	require.Equal(t, []string{"admin", "infrastructure"}, calls)
}
