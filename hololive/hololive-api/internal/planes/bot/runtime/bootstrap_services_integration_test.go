package botruntime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestInitCoreIntegrationServices_PopulatesCommandBuilders(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)

	logger := slog.New(slog.DiscardHandler)
	infra := &sharedmodules.InfraModule{
		Postgres: &dbmocks.Client{
			GetPoolFunc: func() *pgxpool.Pool { return pool },
		},
		Cache: &cachemocks.Client{
			SetFunc:  func(context.Context, string, any, time.Duration) error { return nil },
			DelFunc:  func(context.Context, string) error { return nil },
			SAddFunc: func(context.Context, string, []string) (int64, error) { return 1, nil },
		},
	}

	config := &apiconfig.BotPlaneConfig{Kakao: settings.KakaoConfig{ACLMode: "whitelist"}}
	services, err := appbootstrap.InitCoreIntegrationServices(t.Context(), config, infra, logger)
	require.NoError(t, err)
	require.NotNil(t, services)
	assert.NotNil(t, services.CommandBuilders)
	assert.Empty(t, services.CommandBuilders)
}

func TestCommandBuildersRemainNonNilThroughBootstrapAssembly(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)

	logger := slog.New(slog.DiscardHandler)
	infra := &sharedmodules.InfraModule{
		Postgres: &dbmocks.Client{
			GetPoolFunc: func() *pgxpool.Pool { return pool },
		},
		Cache: &cachemocks.Client{
			SetFunc:  func(context.Context, string, any, time.Duration) error { return nil },
			DelFunc:  func(context.Context, string) error { return nil },
			SAddFunc: func(context.Context, string, []string) (int64, error) { return 1, nil },
		},
	}

	config := &apiconfig.BotPlaneConfig{Kakao: settings.KakaoConfig{ACLMode: "whitelist"}}
	integrationServices, err := appbootstrap.InitCoreIntegrationServices(t.Context(), config, infra, logger)
	require.NoError(t, err)

	deps := appbootstrap.BuildBotDependencies(
		&apiconfig.BotPlaneConfig{},
		&sharedmodules.InfraModule{},
		&apifoundation.ScraperHolodexFoundation{},
		&appbootstrap.AlarmYouTubeStackComponents{AlarmMode: &appbootstrap.AlarmModeComponents{}},
		integrationServices,
		nil,
		nil,
		nil,
		nil,
		logger,
	)

	require.NotNil(t, deps)
	assert.NotNil(t, deps.CommandBuilders)
	assert.Empty(t, deps.CommandBuilders)
}
