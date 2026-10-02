package dispatchstate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validConfig() Config {
	return Config{
		BatchSize:                   50,
		LockTimeout:                 5 * time.Minute,
		PollInterval:                2 * time.Second,
		MaxRetries:                  3,
		RetryBackoff:                time.Minute,
		ReviveInterval:              5 * time.Minute,
		ReviveFreshnessWindow:       time.Hour,
		ClaimFreshnessWindow:        2 * time.Hour,
		DeliveryParallelism:         4,
		DeliverySendTimeout:         10 * time.Second,
		SubscriberLookupParallelism: 16,
		AggregateSyncInterval:       30 * time.Second,
		TelemetryPollInterval:       30 * time.Second,
		TelemetryFlushBatch:         200,
		TelemetryRetryBackoff:       30 * time.Second,
		TelemetryRetention:          24 * time.Hour,
	}
}

func TestConfigValidateAcceptsProfileShapedConfig(t *testing.T) {
	t.Parallel()

	config := validConfig()
	require.NoError(t, config.Validate())
}

func TestConfigValidateRejectsNonPositiveSettings(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*Config){
		"batch size":                    func(c *Config) { c.BatchSize = 0 },
		"lock timeout":                  func(c *Config) { c.LockTimeout = 0 },
		"max retries":                   func(c *Config) { c.MaxRetries = 0 },
		"retry backoff":                 func(c *Config) { c.RetryBackoff = -time.Second },
		"delivery parallelism":          func(c *Config) { c.DeliveryParallelism = 0 },
		"subscriber lookup parallelism": func(c *Config) { c.SubscriberLookupParallelism = 0 },
		"telemetry retention":           func(c *Config) { c.TelemetryRetention = 0 },
	}

	for setting, mutate := range cases {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()

			config := validConfig()
			mutate(&config)
			require.ErrorContains(t, config.Validate(), setting)
		})
	}
}

// claim 신선도 기간이 revive 기간과 주기의 합보다 짧으면 끌어올리지 않고 거절한다.
func TestConfigValidateRejectsClaimWindowBelowReviveWindowPlusInterval(t *testing.T) {
	t.Parallel()

	config := validConfig()

	config.ReviveFreshnessWindow = 60 * time.Minute
	config.ReviveInterval = 5 * time.Minute
	config.ClaimFreshnessWindow = 62 * time.Minute

	require.ErrorContains(t, config.Validate(), "claim freshness window")

	config.ClaimFreshnessWindow = 65 * time.Minute
	require.NoError(t, config.Validate())
}
