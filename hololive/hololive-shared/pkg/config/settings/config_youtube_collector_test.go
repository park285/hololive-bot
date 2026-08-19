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
	if cfg.CollectionOverhead != 5*time.Second || cfg.PublishTimeout != 5*time.Second ||
		cfg.DBTimeout != 5*time.Second || cfg.RenewTimeout != 5*time.Second || cfg.CleanupTimeout != 5*time.Second {
		t.Fatalf("phase timeouts = %#v", cfg)
	}
	if cfg.ReadinessTimeout != 2*time.Second || cfg.HelperHealthTimeout != time.Second {
		t.Fatalf("readiness timeouts = %s %s", cfg.ReadinessTimeout, cfg.HelperHealthTimeout)
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
	cfg.InstanceID = "youtube-collector-c"
	holodex := DefaultHolodexOperationalConfig().Timeout
	official := DefaultOfficialScheduleConfig().Timeout
	if err := cfg.Validate(holodex, official); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}

func TestLoadYouTubeCollectorConfigNonDefaultOverride(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", "youtube-collector-c")
	t.Setenv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", "3")
	t.Setenv("YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS", "1")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS", "40")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS", "41")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_SHUTDOWN_TIMEOUT_SECONDS", "4")
	t.Setenv("YOUTUBE_COLLECTOR_MAX_PAGES", "3")
	t.Setenv("YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES", "65536")
	t.Setenv("YOUTUBE_COLLECTOR_REQUEST_INTERVAL_SECONDS", "5")

	cfg, err := loadYouTubeCollectorConfig()
	if err != nil {
		t.Fatalf("loadYouTubeCollectorConfig() error = %v", err)
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
	if cfg.InstanceID != "youtube-collector-c" {
		t.Fatalf("InstanceID = %q, want youtube-collector-c", cfg.InstanceID)
	}
}

func TestCFG005MaxSuccessResponseBytesDualEnvMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		newRaw, oldRaw string
		hasNew, hasOld bool
		want           int
		wantErr        bool
	}{
		{name: "new only", newRaw: "2048", hasNew: true, want: 2048},
		{name: "old only", oldRaw: "4096", hasOld: true, want: 4096},
		{name: "both equal", newRaw: "8192", oldRaw: "8192", hasNew: true, hasOld: true, want: 8192},
		{name: "both differ", newRaw: "8192", oldRaw: "4096", hasNew: true, hasOld: true, wantErr: true},
		{name: "neither", want: 1024},
		{name: "new explicitly empty", hasNew: true, wantErr: true},
		{name: "old explicitly empty", hasOld: true, wantErr: true},
		{name: "both explicitly empty", hasNew: true, hasOld: true, wantErr: true},
		{name: "new invalid", newRaw: "not-an-integer", hasNew: true, wantErr: true},
		{name: "old zero", oldRaw: "0", hasOld: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveDualPositiveInt(
				"YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES", test.newRaw, test.hasNew,
				"YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES", test.oldRaw, test.hasOld,
				1024,
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("resolve error = %v, wantErr %t", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("resolved value = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCFG005CollectorDurationAliasTruthTable(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		name             string
		newName, oldName string
	}{
		{
			name:    "collection overhead",
			newName: "YOUTUBE_COLLECTOR_COLLECTION_OVERHEAD_SECONDS",
			oldName: "YOUTUBE_COLLECTOR_NORMALIZATION_BUDGET_SECONDS",
		},
		{
			name:    "publish timeout",
			newName: "YOUTUBE_COLLECTOR_PUBLISH_TIMEOUT_SECONDS",
			oldName: "YOUTUBE_COLLECTOR_PUBLISH_BUDGET_SECONDS",
		},
		{
			name:    "youtubejs request timeout",
			newName: "YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS",
			oldName: "YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS",
		},
	}
	cases := []struct {
		name           string
		newRaw, oldRaw string
		hasNew, hasOld bool
		want           time.Duration
		wantErr        bool
	}{
		{name: "new only", newRaw: "7", hasNew: true, want: 7 * time.Second},
		{name: "old only", oldRaw: "11", hasOld: true, want: 11 * time.Second},
		{name: "both equal", newRaw: "13", oldRaw: "13", hasNew: true, hasOld: true, want: 13 * time.Second},
		{name: "both differ", newRaw: "7", oldRaw: "11", hasNew: true, hasOld: true, wantErr: true},
		{name: "neither", want: 5 * time.Second},
		{name: "new explicitly empty", hasNew: true, wantErr: true},
		{name: "old explicitly empty", hasOld: true, wantErr: true},
		{name: "both explicitly empty", hasNew: true, hasOld: true, wantErr: true},
		{name: "new empty old valid", oldRaw: "11", hasNew: true, hasOld: true, wantErr: true},
		{name: "new valid old empty", newRaw: "7", hasNew: true, hasOld: true, wantErr: true},
		{name: "negative", newRaw: "-1", hasNew: true, wantErr: true},
		{name: "out of range", newRaw: "9223372036854775807", hasNew: true, wantErr: true},
	}
	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			t.Parallel()
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()
					got, err := resolveDualPositiveDurationUnit(
						pair.newName, test.newRaw, test.hasNew,
						pair.oldName, test.oldRaw, test.hasOld,
						5*time.Second,
						time.Second,
					)
					if (err != nil) != test.wantErr {
						t.Fatalf("resolve error = %v, wantErr %t", err, test.wantErr)
					}
					if err == nil && got != test.want {
						t.Fatalf("resolved value = %s, want %s", got, test.want)
					}
				})
			}
		})
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
			t.Setenv("YOUTUBE_COLLECTOR_MAX_PAGES", test.value)
			if _, err := requiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_PAGES", 4); err == nil {
				t.Fatal("requiredPositiveIntEnv accepted an explicitly empty value")
			}

			t.Setenv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", test.value)
			if _, err := requiredSecondsDurationEnv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", time.Minute); err == nil {
				t.Fatal("requiredDurationUnitEnv accepted an explicitly empty value")
			}
		})
	}
}

func TestYouTubeCollectorConfigRejectsInvalidBudgets(t *testing.T) {
	base := DefaultYouTubeCollectorConfig()
	base.InstanceID = "youtube-collector-c"
	holodex := 25 * time.Second
	official := 15 * time.Second
	tests := []struct {
		name   string
		mutate func(*YouTubeCollectorConfig)
	}{
		{name: "workers below 1", mutate: func(c *YouTubeCollectorConfig) { c.TotalWorkers = 0 }},
		{name: "inflight above workers", mutate: func(c *YouTubeCollectorConfig) { c.HolodexMaxInflight = c.TotalWorkers + 1 }},
		{name: "pages above 100", mutate: func(c *YouTubeCollectorConfig) { c.MaxPages = 101 }},
		{name: "response above 1MiB", mutate: func(c *YouTubeCollectorConfig) {
			c.MaxSuccessResponseBytes = youtubeCollectorMaxSuccessResponseBytes + 1
		}},
		{name: "renew timing fills ttl", mutate: func(c *YouTubeCollectorConfig) { c.RenewInterval = 55 * time.Second }},
		{name: "database timeout below bound", mutate: func(c *YouTubeCollectorConfig) { c.DBTimeout = 50 * time.Millisecond }},
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

func TestRDY015ProductionInstanceIDRejectedWhenEmptyOrInvalid(t *testing.T) {
	holodex := DefaultHolodexOperationalConfig().Timeout
	official := DefaultOfficialScheduleConfig().Timeout
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
			cfg := DefaultYouTubeCollectorConfig()
			cfg.InstanceID = test.id
			if err := cfg.Validate(holodex, official); err == nil {
				t.Fatal("invalid production instance ID must fail closed")
			}
		})
	}
	cfg := DefaultYouTubeCollectorConfig()
	cfg.InstanceID = "youtube-collector-c"
	if err := cfg.Validate(holodex, official); err != nil {
		t.Fatalf("valid production instance ID must be accepted: %v", err)
	}
}
