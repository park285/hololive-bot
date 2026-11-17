package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

const preflightBotPool = "bot"

func TestRuntimeDatabasePreflightUsesPoolReservation(t *testing.T) {
	t.Parallel()

	config := settings.PostgresConfig{
		Host: "database.invalid", Port: 1, User: "test", Database: "test", SSLMode: "disable",
		PoolMinConns: 0, PoolMaxConns: 1,
	}
	require.NoError(t, Preflight(&config, RuntimePool{Name: "admin"}))
	require.ErrorContains(t, Preflight(&config, RuntimePool{Name: preflightBotPool, ReservedConns: 1}), "acquire policy")

	config.PoolMaxConns = 2
	require.NoError(t, Preflight(&config, RuntimePool{Name: preflightBotPool, ReservedConns: 1}))
	require.Error(t, Preflight(nil, RuntimePool{Name: preflightBotPool, ReservedConns: 1}))
}

func TestRuntimeAndMaintenanceDatabasePoliciesStaySeparate(t *testing.T) {
	t.Setenv("PGAPPNAME", "hololive-api")

	config := settings.PostgresConfig{PoolMinConns: 0, PoolMaxConns: 2}
	runtime := runtimePostgresConfig(&config, RuntimePool{Name: preflightBotPool, ReservedConns: 1})
	require.Equal(t, "hololive-api/bot", runtime.RuntimeParams["application_name"])
	require.Equal(t, "30000", runtime.RuntimeParams["statement_timeout"])
	require.Equal(t, int32(1), runtime.AcquirePolicy.ReservedConns)
	require.Equal(t, 5*time.Second, runtime.AcquirePolicy.Timeout)
	require.Equal(t, time.Second, runtime.CancelRequest.DeadlineDelay)
	require.Zero(t, runtime.PoolMinConns)
	require.Equal(t, 2, runtime.PoolMaxConns)

	maintenance := servicePostgresConfig(&config)
	require.Nil(t, maintenance.RuntimeParams)
	require.Nil(t, maintenance.AcquirePolicy)
	require.Nil(t, maintenance.CancelRequest)
}
