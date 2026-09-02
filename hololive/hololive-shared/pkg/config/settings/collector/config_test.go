package collector

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func TestDefaultYouTubeCollectorConfigMatchesCurrentBehavior(t *testing.T) {
	cfg := DefaultConfig()
	workers := settings.DefaultScraperWorkerCount()

	assertCollectorWorkerDefaults(t, cfg, workers)
	assertCollectorLeaseDefaults(t, cfg)
	assertCollectorTimeoutDefaults(t, cfg)
	assertCollectorRetryDefaults(t, cfg, settings.DefaultScraperSchedulerConfig())
	assertCollectorLimitDefaults(t, cfg, workers)

	cfg.InstanceID = settingstest.CollectorInstanceC

	holodex := settings.DefaultHolodexOperationalConfig().Timeout
	official := settings.DefaultOfficialScheduleConfig().Timeout

	if err := cfg.Validate(holodex, official); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}

func assertCollectorWorkerDefaults(t *testing.T, cfg Config, workers int) {
	t.Helper()

	if cfg.TotalWorkers != workers {
		t.Fatalf("TotalWorkers = %d, want %d", cfg.TotalWorkers, workers)
	}

	if cfg.QueueCapacity != workers*4 {
		t.Fatalf("QueueCapacity = %d, want %d", cfg.QueueCapacity, workers*4)
	}

	if cfg.AcquisitionBatch != cfg.QueueCapacity {
		t.Fatalf("AcquisitionBatch = %d, want %d", cfg.AcquisitionBatch, cfg.QueueCapacity)
	}
}

func assertCollectorLeaseDefaults(t *testing.T, cfg Config) {
	t.Helper()

	if cfg.AcquisitionCadence != time.Second || cfg.LeaseTTL != time.Minute || cfg.RenewInterval != 20*time.Second {
		t.Fatalf("cadence/ttl/renew = %s %s %s", cfg.AcquisitionCadence, cfg.LeaseTTL, cfg.RenewInterval)
	}
}

func assertCollectorTimeoutDefaults(t *testing.T, cfg Config) {
	t.Helper()

	if cfg.CollectionOverhead != 5*time.Second || cfg.PublishTimeout != 5*time.Second ||
		cfg.DBTimeout != 5*time.Second || cfg.RenewTimeout != 5*time.Second || cfg.CleanupTimeout != 5*time.Second {
		t.Fatalf("phase timeouts = %#v", cfg)
	}

	if cfg.ReadinessTimeout != 2*time.Second || cfg.HelperHealthTimeout != time.Second {
		t.Fatalf("readiness timeouts = %s %s", cfg.ReadinessTimeout, cfg.HelperHealthTimeout)
	}
}

func assertCollectorRetryDefaults(t *testing.T, cfg Config, retry settings.ScraperSchedulerConfig) {
	t.Helper()

	if cfg.RetryMin != retry.ErrorBackoffMin || cfg.RetryMax != retry.ErrorBackoffMax {
		t.Fatalf("retry = %s %s", cfg.RetryMin, cfg.RetryMax)
	}

	if cfg.ReleaseJitterMin != 100*time.Millisecond || cfg.ReleaseJitterMax != time.Second {
		t.Fatalf("jitter = %s %s", cfg.ReleaseJitterMin, cfg.ReleaseJitterMax)
	}
}

func assertCollectorLimitDefaults(t *testing.T, cfg Config, workers int) {
	t.Helper()

	if cfg.HolodexMaxInflight != workers || cfg.OfficialMaxInflight != workers || cfg.YouTubeJSMaxInflight != workers {
		t.Fatalf("inflight = %d %d %d", cfg.HolodexMaxInflight, cfg.OfficialMaxInflight, cfg.YouTubeJSMaxInflight)
	}

	if cfg.YouTubeJSRequestTimeout != 30*time.Second || cfg.MaxPages != 1 ||
		cfg.MaxSuccessResponseBytes != youtubeCollectorMaxSuccessResponseBytes {
		t.Fatalf("helper bounds = %s %d %d", cfg.YouTubeJSRequestTimeout, cfg.MaxPages, cfg.MaxSuccessResponseBytes)
	}

	if cfg.RequestInterval != 2*time.Second {
		t.Fatalf("RequestInterval = %s, want 2s", cfg.RequestInterval)
	}

	if cfg.InstanceID != "" {
		t.Fatalf("InstanceID = %q, want empty default", cfg.InstanceID)
	}
}

func TestLoadYouTubeCollectorConfigNonDefaultOverride(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", settingstest.CollectorInstanceC)
	t.Setenv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", "3")
	t.Setenv("YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS", "1")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS", "40")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS", "41")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_SHUTDOWN_TIMEOUT_SECONDS", "4")
	t.Setenv("YOUTUBE_COLLECTOR_MAX_PAGES", "3")
	t.Setenv("YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES", "65536")
	t.Setenv("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS", "5")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}

	if cfg.TotalWorkers != 0 || cfg.QueueCapacity != 0 || cfg.AcquisitionBatch != 0 {
		t.Fatalf("worker-owned fields = %d %d %d, want zero before profile apply", cfg.TotalWorkers, cfg.QueueCapacity, cfg.AcquisitionBatch)
	}

	if cfg.YouTubeJSRequestTimeout != 40*time.Second || cfg.MaxPages != 3 || cfg.MaxSuccessResponseBytes != 65536 {
		t.Fatalf("helper bounds = %s %d %d", cfg.YouTubeJSRequestTimeout, cfg.MaxPages, cfg.MaxSuccessResponseBytes)
	}

	if cfg.RequestInterval != 5*time.Second {
		t.Fatalf("RequestInterval = %s, want 5s", cfg.RequestInterval)
	}

	if cfg.ReadinessTimeout != 3*time.Second || cfg.HelperHealthTimeout != time.Second {
		t.Fatalf("readiness timeouts = %s %s", cfg.ReadinessTimeout, cfg.HelperHealthTimeout)
	}

	if cfg.InstanceID != settingstest.CollectorInstanceC {
		t.Fatalf("InstanceID = %q, want youtube-collector-c", cfg.InstanceID)
	}
}

func TestLoadYouTubeCollectorConfigIgnoresRetiredAliasEnv(t *testing.T) {
	for _, name := range []string{
		"YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES",
		"YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES",
		"YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS",
		"YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS",
	} {
		settingstest.UnsetEnv(t, name)
	}

	t.Setenv("YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES", "4096")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS", "11")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}

	defaults := DefaultConfig()
	if cfg.MaxSuccessResponseBytes != defaults.MaxSuccessResponseBytes {
		t.Fatalf("MaxSuccessResponseBytes = %d, want default %d", cfg.MaxSuccessResponseBytes, defaults.MaxSuccessResponseBytes)
	}

	if cfg.YouTubeJSRequestTimeout != defaults.YouTubeJSRequestTimeout {
		t.Fatalf("YouTubeJSRequestTimeout = %s, want default %s", cfg.YouTubeJSRequestTimeout, defaults.YouTubeJSRequestTimeout)
	}
}

func TestRequiredCollectorNumericEnvRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "whitespace", value: "   "},
		{name: "not an integer", value: "not-an-integer"},
		{name: "zero", value: "0"},
		{name: "negative", value: "-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YOUTUBE_COLLECTOR_MAX_PAGES", test.value)

			if _, err := load.RequiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_PAGES", 4); err == nil {
				t.Fatalf("load.RequiredPositiveIntEnv accepted %q", test.value)
			}

			t.Setenv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", test.value)

			if _, err := load.RequiredSecondsDurationEnv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", time.Minute); err == nil {
				t.Fatalf("load.RequiredSecondsDurationEnv accepted %q", test.value)
			}
		})
	}
}

func TestYouTubeCollectorConfigRejectsInvalidBudgets(t *testing.T) {
	base := DefaultConfig()

	base.InstanceID = settingstest.CollectorInstanceC

	holodex := 25 * time.Second
	official := 15 * time.Second
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "workers below 1", mutate: func(c *Config) { c.TotalWorkers = 0 }},
		{name: "inflight above workers", mutate: func(c *Config) { c.HolodexMaxInflight = c.TotalWorkers + 1 }},
		{name: "pages above 100", mutate: func(c *Config) { c.MaxPages = 101 }},
		{name: "response above 1MiB", mutate: func(c *Config) {
			c.MaxSuccessResponseBytes = youtubeCollectorMaxSuccessResponseBytes + 1
		}},
		{name: "renew timing fills ttl", mutate: func(c *Config) { c.RenewInterval = 55 * time.Second }},
		{name: "database timeout below bound", mutate: func(c *Config) { c.DBTimeout = 50 * time.Millisecond }},
		{name: "retry inverted", mutate: func(c *Config) { c.RetryMin = time.Minute; c.RetryMax = 30 * time.Second }},
		{name: "jitter inverted", mutate: func(c *Config) {
			c.ReleaseJitterMin = time.Second
			c.ReleaseJitterMax = 100 * time.Millisecond
		}},
		{name: "request interval below 1s", mutate: func(c *Config) { c.RequestInterval = 500 * time.Millisecond }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.mutate(&cfg)

			if err := cfg.Validate(holodex, official); err == nil {
				t.Fatal("invalid collector budget must fail closed")
			}
		})
	}
}

func TestRDY015ProductionInstanceIDRejectedWhenEmptyOrInvalid(t *testing.T) {
	holodex := settings.DefaultHolodexOperationalConfig().Timeout
	official := settings.DefaultOfficialScheduleConfig().Timeout
	tests := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "uppercase", id: "YouTube-Collector-C"},
		{name: "underscore", id: "youtube_collector_c"},
		{name: "too long", id: strings.Repeat("a", 65)},
		{name: "leading hyphen", id: "-collector"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()

			cfg.InstanceID = test.id

			if err := cfg.Validate(holodex, official); err == nil {
				t.Fatal("invalid production instance ID must fail closed")
			}
		})
	}

	cfg := DefaultConfig()

	cfg.InstanceID = settingstest.CollectorInstanceC

	if err := cfg.Validate(holodex, official); err != nil {
		t.Fatalf("valid production instance ID must be accepted: %v", err)
	}
}
