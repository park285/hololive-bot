package settings

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultYouTubeCollectorConfigMatchesCurrentBehavior(t *testing.T) {
	cfg := DefaultYouTubeCollectorConfig()
	workers := DefaultScraperWorkerCount()
	retry := DefaultScraperSchedulerConfig()
	if cfg.TotalWorkers != workers {
		t.Fatalf("TotalWorkers = %d, want %d", cfg.TotalWorkers, workers)
	}
	if cfg.QueueCapacity != workers*4 {
		t.Fatalf("QueueCapacity = %d, want %d", cfg.QueueCapacity, workers*4)
	}
	if cfg.AcquisitionBatch != cfg.QueueCapacity {
		t.Fatalf("AcquisitionBatch = %d, want %d", cfg.AcquisitionBatch, cfg.QueueCapacity)
	}
	if cfg.AcquisitionCadence != time.Second || cfg.LeaseTTL != time.Minute || cfg.RenewInterval != 20*time.Second {
		t.Fatalf("cadence/ttl/renew = %s %s %s", cfg.AcquisitionCadence, cfg.LeaseTTL, cfg.RenewInterval)
	}
	if cfg.NormalizationBudget != 5*time.Second || cfg.PublishBudget != 5*time.Second {
		t.Fatalf("budgets = %s %s", cfg.NormalizationBudget, cfg.PublishBudget)
	}
	if cfg.RetryMin != retry.ErrorBackoffMin || cfg.RetryMax != retry.ErrorBackoffMax {
		t.Fatalf("retry = %s %s", cfg.RetryMin, cfg.RetryMax)
	}
	if cfg.ReleaseJitterMin != 100*time.Millisecond || cfg.ReleaseJitterMax != time.Second {
		t.Fatalf("jitter = %s %s", cfg.ReleaseJitterMin, cfg.ReleaseJitterMax)
	}
	if cfg.HolodexMaxInflight != workers || cfg.OfficialMaxInflight != workers || cfg.YouTubeJSMaxInflight != workers {
		t.Fatalf("inflight = %d %d %d", cfg.HolodexMaxInflight, cfg.OfficialMaxInflight, cfg.YouTubeJSMaxInflight)
	}
	if cfg.YouTubeJSTimeout != 30*time.Second || cfg.MaxPages != 1 || cfg.MaxAggregateBytes != youtubeCollectorMaxAggregateBytes {
		t.Fatalf("helper bounds = %s %d %d", cfg.YouTubeJSTimeout, cfg.MaxPages, cfg.MaxAggregateBytes)
	}
	if cfg.RequestInterval != 2*time.Second {
		t.Fatalf("RequestInterval = %s, want 2s", cfg.RequestInterval)
	}
	holodex := DefaultHolodexOperationalConfig().Timeout
	official := DefaultOfficialScheduleConfig().Timeout
	if err := cfg.Validate(holodex, official); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}

func TestLoadYouTubeCollectorConfigNonDefaultOverride(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_TOTAL_WORKERS", "8")
	t.Setenv("YOUTUBE_COLLECTOR_QUEUE_CAPACITY", "24")
	t.Setenv("YOUTUBE_COLLECTOR_ACQUISITION_BATCH", "12")
	t.Setenv("YOUTUBE_COLLECTOR_ACQUISITION_CADENCE_MS", "250")
	t.Setenv("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS", "90")
	t.Setenv("YOUTUBE_COLLECTOR_RENEW_INTERVAL_SECONDS", "15")
	t.Setenv("YOUTUBE_COLLECTOR_NORMALIZATION_BUDGET_SECONDS", "8")
	t.Setenv("YOUTUBE_COLLECTOR_PUBLISH_BUDGET_SECONDS", "7")
	t.Setenv("YOUTUBE_COLLECTOR_RETRY_MIN_SECONDS", "45")
	t.Setenv("YOUTUBE_COLLECTOR_RETRY_MAX_SECONDS", "180")
	t.Setenv("YOUTUBE_COLLECTOR_RELEASE_JITTER_MIN_MS", "200")
	t.Setenv("YOUTUBE_COLLECTOR_RELEASE_JITTER_MAX_MS", "800")
	t.Setenv("YOUTUBE_COLLECTOR_HOLODEX_MAX_INFLIGHT", "3")
	t.Setenv("YOUTUBE_COLLECTOR_OFFICIAL_MAX_INFLIGHT", "2")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_MAX_INFLIGHT", "4")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS", "40")
	t.Setenv("YOUTUBE_COLLECTOR_MAX_PAGES", "3")
	t.Setenv("YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES", "65536")
	t.Setenv("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS", "5")

	cfg, err := loadYouTubeCollectorConfig()
	if err != nil {
		t.Fatalf("loadYouTubeCollectorConfig() error = %v", err)
	}
	if cfg.TotalWorkers != 8 || cfg.QueueCapacity != 24 || cfg.AcquisitionBatch != 12 {
		t.Fatalf("workers/queue/batch = %d %d %d", cfg.TotalWorkers, cfg.QueueCapacity, cfg.AcquisitionBatch)
	}
	if cfg.AcquisitionCadence != 250*time.Millisecond || cfg.LeaseTTL != 90*time.Second || cfg.RenewInterval != 15*time.Second {
		t.Fatalf("cadence/ttl/renew = %s %s %s", cfg.AcquisitionCadence, cfg.LeaseTTL, cfg.RenewInterval)
	}
	if cfg.HolodexMaxInflight != 3 || cfg.OfficialMaxInflight != 2 || cfg.YouTubeJSMaxInflight != 4 {
		t.Fatalf("inflight = %d %d %d", cfg.HolodexMaxInflight, cfg.OfficialMaxInflight, cfg.YouTubeJSMaxInflight)
	}
	if cfg.YouTubeJSTimeout != 40*time.Second || cfg.MaxPages != 3 || cfg.MaxAggregateBytes != 65536 {
		t.Fatalf("helper bounds = %s %d %d", cfg.YouTubeJSTimeout, cfg.MaxPages, cfg.MaxAggregateBytes)
	}
	if cfg.RequestInterval != 5*time.Second {
		t.Fatalf("RequestInterval = %s, want 5s", cfg.RequestInterval)
	}
	if err := cfg.Validate(25*time.Second, 15*time.Second); err != nil {
		t.Fatalf("overridden config must be valid: %v", err)
	}
}

func TestLoadYouTubeCollectorConfigRejectsInvalidExplicitValues(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_TOTAL_WORKERS", "0")
	if _, err := loadYouTubeCollectorConfig(); err == nil {
		t.Fatal("explicit zero workers must fail closed")
	}
	t.Setenv("YOUTUBE_COLLECTOR_TOTAL_WORKERS", "4")
	t.Setenv("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS", "abc")
	if _, err := loadYouTubeCollectorConfig(); err == nil {
		t.Fatal("unparseable lease TTL must fail closed")
	}
}

func TestRequiredCollectorNumericEnvRejectsExplicitEmptyValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "whitespace", value: "   "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YOUTUBE_COLLECTOR_TOTAL_WORKERS", test.value)
			if _, err := requiredPositiveIntEnv("YOUTUBE_COLLECTOR_TOTAL_WORKERS", 4); err == nil {
				t.Fatal("requiredPositiveIntEnv accepted an explicitly empty value")
			}

			t.Setenv("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS", test.value)
			if _, err := requiredDurationUnitEnv("YOUTUBE_COLLECTOR_LEASE_TTL_SECONDS", time.Minute, time.Second); err == nil {
				t.Fatal("requiredDurationUnitEnv accepted an explicitly empty value")
			}
		})
	}
}

func TestYouTubeCollectorConfigRejectsInvalidBudgets(t *testing.T) {
	base := DefaultYouTubeCollectorConfig()
	holodex := 25 * time.Second
	official := 15 * time.Second
	tests := []struct {
		name   string
		mutate func(*YouTubeCollectorConfig)
	}{
		{name: "workers below 1", mutate: func(c *YouTubeCollectorConfig) { c.TotalWorkers = 0 }},
		{name: "inflight above workers", mutate: func(c *YouTubeCollectorConfig) { c.HolodexMaxInflight = c.TotalWorkers + 1 }},
		{name: "pages above 100", mutate: func(c *YouTubeCollectorConfig) { c.MaxPages = 101 }},
		{name: "aggregate above 1MiB", mutate: func(c *YouTubeCollectorConfig) { c.MaxAggregateBytes = youtubeCollectorMaxAggregateBytes + 1 }},
		{name: "renew above ttl/3", mutate: func(c *YouTubeCollectorConfig) { c.RenewInterval = 21 * time.Second }},
		{name: "timeout fills ttl", mutate: func(c *YouTubeCollectorConfig) { c.YouTubeJSTimeout = 50 * time.Second }},
		{name: "retry inverted", mutate: func(c *YouTubeCollectorConfig) { c.RetryMin = time.Minute; c.RetryMax = 30 * time.Second }},
		{name: "jitter inverted", mutate: func(c *YouTubeCollectorConfig) {
			c.ReleaseJitterMin = time.Second
			c.ReleaseJitterMax = 100 * time.Millisecond
		}},
		{name: "request interval below 1s", mutate: func(c *YouTubeCollectorConfig) { c.RequestInterval = 500 * time.Millisecond }},
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

func TestValidateYouTubeCollectorRuntimeAppliesCollectorConfig(t *testing.T) {
	clearRuntimeRoleEnv(t)
	cfg := validRuntimeRoleConfig()
	cfg.Postgres.User = "hololive_scraper"
	cfg.Holodex.APIKey = ""
	cfg.Holodex.Timeout = 25 * time.Second
	cfg.YouTubeCollector.TotalWorkers = 8
	cfg.YouTubeCollector.YouTubeJSTimeout = 40 * time.Second
	if err := cfg.ValidateYouTubeCollectorRuntime(); err != nil {
		t.Fatalf("ValidateYouTubeCollectorRuntime() error = %v", err)
	}
	if cfg.YouTubeCollector.TotalWorkers != 8 || cfg.YouTubeCollector.QueueCapacity != 32 {
		t.Fatalf("applied collector config = %#v", cfg.YouTubeCollector)
	}
	if cfg.YouTubeCollector.HolodexMaxInflight != 8 || cfg.YouTubeCollector.YouTubeJSTimeout != 40*time.Second {
		t.Fatalf("applied inflight/timeout = %d %s", cfg.YouTubeCollector.HolodexMaxInflight, cfg.YouTubeCollector.YouTubeJSTimeout)
	}
}

func TestValidateYouTubeCollectorRuntimeRejectsInvalidCollectorBudget(t *testing.T) {
	clearRuntimeRoleEnv(t)
	cfg := validRuntimeRoleConfig()
	cfg.Postgres.User = "hololive_scraper"
	cfg.Holodex.Timeout = 25 * time.Second
	cfg.YouTubeCollector = DefaultYouTubeCollectorConfig()
	cfg.YouTubeCollector.YouTubeJSTimeout = 55 * time.Second
	err := cfg.ValidateYouTubeCollectorRuntime()
	if err == nil || !strings.Contains(err.Error(), "LEASE_TTL") {
		t.Fatalf("ValidateYouTubeCollectorRuntime() error = %v, want lease TTL rejection", err)
	}
}
