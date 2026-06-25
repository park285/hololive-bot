package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAlarmProviderURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment string
		url         string
		wantErr     string
	}{
		{name: "development http", environment: "development", url: "http://127.0.0.1:30007"},
		{name: "production https", environment: "production", url: "https://hololive-alarm-worker:30007"},
		{name: "missing", environment: "production", wantErr: "required"},
		{name: "missing host", environment: "development", url: "https:///alarm", wantErr: "include a host"},
		{name: "production http", environment: "production", url: "http://hololive-alarm-worker:30007", wantErr: "must use https"},
		{name: "unsupported scheme", environment: "development", url: "grpc://alarm:30007", wantErr: "scheme must be http or https"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAlarmProviderURL(tt.environment, tt.url)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateHololiveAPIListenerPorts(t *testing.T) {
	t.Parallel()

	config := &HololiveAPIConfig{
		Bot:   &Config{Server: ServerConfig{Port: 30001, MetricsAddr: ":30091", PprofAddr: ":30061"}},
		Admin: &Config{Server: ServerConfig{Port: 30006}},
		LLM:   &LLMSchedulerConfig{Server: ServerConfig{Port: 30003}},
	}
	require.NoError(t, validateHololiveAPIListenerPorts(config))

	config.Admin.Server.Port = 30001
	err := validateHololiveAPIListenerPorts(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared by bot and admin")
}

func TestValidatePlanePool(t *testing.T) {
	t.Parallel()

	require.NoError(t, validatePlanePool("bot", PostgresConfig{PoolMinConns: 1, PoolMaxConns: 4}))
	require.Error(t, validatePlanePool("bot", PostgresConfig{PoolMinConns: 5, PoolMaxConns: 4}))
	require.Error(t, validatePlanePool("bot", PostgresConfig{PoolMinConns: 0, PoolMaxConns: 0}))
}
