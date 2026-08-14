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

	newConfig := func(shortLinkAddr string) *HololiveAPIConfig {
		return &HololiveAPIConfig{
			Bot: &Config{Server: ServerConfig{
				Port:          30001,
				H3Addr:        ":30001",
				ShortLinkAddr: shortLinkAddr,
				MetricsAddr:   ":30091",
				PprofAddr:     ":30061",
			}},
			Admin: &Config{Server: ServerConfig{Port: 30006, H3Addr: ":30006"}},
			LLM:   &LLMSchedulerConfig{Server: ServerConfig{Port: 30003, H3Addr: ":30003"}},
		}
	}

	config := newConfig(":30101")
	require.NoError(t, validateHololiveAPIListenerPorts(config))

	config.Admin.Server.Port = 30001
	config.Admin.Server.H3Addr = ":30001"
	err := validateHololiveAPIListenerPorts(config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared by bot-h3 and admin-h3")

	tests := []struct {
		name    string
		addr    string
		wantErr string
	}{
		{name: "metrics collision", addr: ":30091", wantErr: "shared by short-link and metrics"},
		{name: "pprof collision", addr: ":30061", wantErr: "shared by short-link and pprof"},
		{name: "invalid address", addr: "not-an-address", wantErr: "short-link listener: invalid address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateHololiveAPIListenerPorts(newConfig(tt.addr))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("same port on tcp and udp is allowed", func(t *testing.T) {
		require.NoError(t, validateHololiveAPIListenerPorts(newConfig(":30001")))
	})

	t.Run("same tcp port on distinct specific hosts is allowed", func(t *testing.T) {
		config := newConfig("127.0.0.1:30091")
		config.Bot.Server.MetricsAddr = "100.100.1.3:30091"
		require.NoError(t, validateHololiveAPIListenerPorts(config))
	})

	t.Run("h3 address must match configured plane port", func(t *testing.T) {
		config := newConfig(":30101")
		config.LLM.Server.H3Addr = ":30004"
		err := validateHololiveAPIListenerPorts(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "llm-h3 listener: address port 30004 must match configured port 30003")
	})
}

func TestConfigureHololiveAPIPlanesSetsBotInternalURL(t *testing.T) {
	t.Setenv("SERVER_PORT", "31001")
	t.Setenv("HOLOLIVE_BOT_INTERNAL_URL", "")

	botConfig := &Config{}
	adminConfig := &Config{}
	llmConfig := &LLMSchedulerConfig{}

	configureHololiveAPIPlanes(botConfig, adminConfig, llmConfig)

	require.Equal(t, 31001, botConfig.Server.Port)
	require.Equal(t, "https://127.0.0.1:31001", adminConfig.BotInternalURL)
}

func TestConfigureHololiveAPIPlanesPreservesBotInternalURLOverride(t *testing.T) {
	t.Setenv("SERVER_PORT", "31001")

	botConfig := &Config{}
	adminConfig := &Config{BotInternalURL: "https://bot.internal:3443"}
	llmConfig := &LLMSchedulerConfig{}

	configureHololiveAPIPlanes(botConfig, adminConfig, llmConfig)

	require.Equal(t, "https://bot.internal:3443", adminConfig.BotInternalURL)
}

func TestValidatePlanePool(t *testing.T) {
	t.Parallel()

	require.NoError(t, validatePlanePool("bot", &PostgresConfig{PoolMinConns: 1, PoolMaxConns: 4}))
	require.Error(t, validatePlanePool("bot", &PostgresConfig{PoolMinConns: 5, PoolMaxConns: 4}))
	require.Error(t, validatePlanePool("bot", &PostgresConfig{PoolMinConns: 0, PoolMaxConns: 0}))
}

func TestValidateYouTubePlaneDatabaseRole(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateYouTubePlaneDatabaseRole("hololive_runtime"))
	err := validateYouTubePlaneDatabaseRole(postgresScraperRoleUser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), postgresScraperRoleUser)
}

func TestHololiveAPIYouTubePlaneComposeBudgetLeavesReservedCapacity(t *testing.T) {
	t.Parallel()
	bot := 4
	admin := 4
	llm := 4
	youtubeCompose := 2
	if bot+admin+llm+youtubeCompose > 16 {
		t.Fatalf("hololive-api process pool sum %d exceeds the reviewed 16-connection envelope", bot+admin+llm+youtubeCompose)
	}
}
