package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
)

func buildInfraFailureConfig() *config.Config {
	return &config.Config{
		Bot: config.BotConfig{
			IngestionEnabled: true,
		},
		Server: config.ServerConfig{
			Port: 30004,
		},
		Valkey: config.ValkeyConfig{
			Host: "127.0.0.1",
			Port: 1,
			DB:   0,
		},
		Postgres: config.PostgresConfig{
			Host:      "127.0.0.1",
			Port:      1,
			User:      "test",
			Password:  "test",
			Database:  "test",
			SSLMode:   "disable",
			QueryExecMode: "simple_protocol",
		},
		Notification: config.NotificationConfig{
			AdvanceMinutes: []int{5},
		},
		Holodex: config.HolodexConfig{
			BaseURL: "https://example.com",
			APIKeys: []string{"dummy"},
		},
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestInitInfraResources_ReturnsErrorOnCanceledContext(t *testing.T) {
	t.Parallel()

	infra, err := initInfraResources(canceledContext(), buildInfraFailureConfig(), testLogger())
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.Contains(t, err.Error(), "provide cache resources")
}

func TestInitStreamIngesterInfrastructure_ReturnsErrorOnCanceledContext(t *testing.T) {
	t.Parallel()

	infra, err := initStreamIngesterInfrastructure(canceledContext(), buildInfraFailureConfig(), testLogger())
	require.Error(t, err)
	assert.Nil(t, infra)
	assert.Contains(t, err.Error(), "provide cache resources")
}

func TestBuildStreamIngesterRuntime_ReturnsErrorOnInfraInitFailure(t *testing.T) {
	t.Parallel()

	runtime, err := BuildStreamIngesterRuntime(canceledContext(), buildInfraFailureConfig(), testLogger())
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "provide cache resources")
}

