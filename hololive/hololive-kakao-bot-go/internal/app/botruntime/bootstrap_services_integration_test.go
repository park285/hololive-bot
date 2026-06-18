package botruntime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-shared/pkg/config"
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

	services, err := appbootstrap.InitCoreIntegrationServices(t.Context(), &config.Config{}, infra, logger)
	require.NoError(t, err)
	require.NotNil(t, services)
	require.NotNil(t, services.WorkerPool)
	assert.NotNil(t, services.CommandBuilders)
	assert.Len(t, services.CommandBuilders, 0)
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

	integrationServices, err := appbootstrap.InitCoreIntegrationServices(t.Context(), &config.Config{}, infra, logger)
	require.NoError(t, err)

	modules := buildBotDependencyModules(
		&config.Config{},
		&sharedmodules.InfraModule{},
		&appbootstrap.AlarmModeComponents{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		integrationServices.ACLService,
		integrationServices.MajorEventRepository,
		integrationServices.MemberNewsService,
		integrationServices.CommandBuilders,
		integrationServices.WorkerPool,
		logger,
	)
	deps := appbootstrap.ProvideBotDependencies(&modules)

	require.NotNil(t, deps)
	assert.NotNil(t, deps.CommandBuilders)
	assert.Len(t, deps.CommandBuilders, 0)
}
