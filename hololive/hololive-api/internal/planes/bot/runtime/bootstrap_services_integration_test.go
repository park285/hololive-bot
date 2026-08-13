package botruntime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
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

	config := &settings.Config{Kakao: settings.KakaoConfig{ACLMode: "whitelist"}}
	services, err := appbootstrap.InitCoreIntegrationServices(t.Context(), config, infra, logger)
	require.NoError(t, err)
	require.NotNil(t, services)
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

	config := &settings.Config{Kakao: settings.KakaoConfig{ACLMode: "whitelist"}}
	integrationServices, err := appbootstrap.InitCoreIntegrationServices(t.Context(), config, infra, logger)
	require.NoError(t, err)

	modules := buildBotDependencyModules(
		&settings.Config{},
		&sharedmodules.InfraModule{},
		&appbootstrap.ScraperHolodexProfileFoundation{},
		&appbootstrap.AlarmYouTubeStackComponents{AlarmMode: &appbootstrap.AlarmModeComponents{}},
		integrationServices,
		nil,
		nil,
		nil,
		nil,
		logger,
	)
	deps := appbootstrap.ProvideBotDependencies(&modules)

	require.NotNil(t, deps)
	assert.NotNil(t, deps.CommandBuilders)
	assert.Len(t, deps.CommandBuilders, 0)
}
