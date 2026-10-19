package config

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestCollectorProfileFixtureAndEnvironmentDefaultsAreValid(t *testing.T) {
	cfg := fixtureCollectorConfig(t)

	cfg.InstanceID = collectorInstanceC

	holodex := settings.DefaultHolodexOperationalConfig().Timeout
	official := settings.DefaultOfficialScheduleConfig().Timeout

	if err := cfg.Validate(holodex, official); err != nil {
		t.Fatalf("profile fixture with environment defaults must be valid: %v", err)
	}
}

func TestEnvironmentDefaultsDoNotSupplyWorkerPolicy(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TotalWorkers != 0 || cfg.QueueCapacity != 0 || cfg.QueueMaxAge != 0 || cfg.AcquisitionBatch != 0 ||
		cfg.DBTimeout != 0 || cfg.LeaseTTL != 0 || cfg.RetryMin != 0 || cfg.HolodexMaxInflight != 0 {
		t.Fatalf("environment defaults supplied worker policy: %+v", cfg)
	}

	if err := cfg.validateProfilePolicy(); err == nil {
		t.Fatal("environment-only defaults accepted without worker profile")
	}
}

func TestLoadYouTubeCollectorConfigNonDefaultOverride(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", collectorInstanceC)
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

	if cfg.InstanceID != collectorInstanceC {
		t.Fatalf("InstanceID = %q, want youtube-collector-c", cfg.InstanceID)
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

			if _, err := envload.RequiredPositiveIntEnv("YOUTUBE_COLLECTOR_MAX_PAGES", 4); err == nil {
				t.Fatalf("envload.RequiredPositiveIntEnv accepted %q", test.value)
			}

			t.Setenv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", test.value)

			if _, err := envload.RequiredSecondsDurationEnv("YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS", time.Minute); err == nil {
				t.Fatalf("envload.RequiredSecondsDurationEnv accepted %q", test.value)
			}
		})
	}
}

func TestYouTubeCollectorConfigRejectsInvalidBudgets(t *testing.T) {
	base := fixtureCollectorConfig(t)

	base.InstanceID = collectorInstanceC

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
		{name: "queue max age unset", mutate: func(c *Config) { c.QueueMaxAge = 0 }},
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

// 단계별 timeout 검증 오류는 운영자가 고칠 설정 이름을 가리켜야 한다. 같은 함수가 검사하는 다른 필드로
// 원인을 오인하지 않도록 필드마다 고유한 이름이 오류에 남는지 확인한다.
func TestYouTubeCollectorConfigPhaseTimeoutErrorsNameField(t *testing.T) {
	base := fixtureCollectorConfig(t)

	base.InstanceID = collectorInstanceC

	holodex := 25 * time.Second
	official := 15 * time.Second
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{name: "cleanup above 1m", mutate: func(c *Config) { c.CleanupTimeout = 2 * time.Minute }, wantSub: "collection.settings.cleanup_timeout_ms must be between 100ms and 1m0s"},
		{name: "admission below bound", mutate: func(c *Config) { c.ProviderAdmissionTimeout = time.Millisecond }, wantSub: "collection.settings.provider_admission_timeout_ms must be between 100ms and 10m0s"},
		{name: "overhead zero", mutate: func(c *Config) { c.CollectionOverhead = 0 }, wantSub: "collection.settings.collection_overhead_ms must be positive"},
		{name: "publish above 5m", mutate: func(c *Config) { c.PublishTimeout = 6 * time.Minute }, wantSub: "collection.settings.publish_timeout_ms must be between 100ms and 5m0s"},
		{name: "readiness above 10s", mutate: func(c *Config) { c.ReadinessTimeout = 11 * time.Second }, wantSub: "YOUTUBE_COLLECTOR_READINESS_TIMEOUT_SECONDS must be between 100ms and 10s"},
		{name: "helper health not below readiness", mutate: func(c *Config) { c.HelperHealthTimeout = c.ReadinessTimeout }, wantSub: "YOUTUBE_COLLECTOR_HELPER_HEALTH_TIMEOUT_SECONDS must be at least 100ms and less than"},
		{name: "youtubejs request above 10m", mutate: func(c *Config) { c.YouTubeJSRequestTimeout = 11 * time.Minute }, wantSub: "YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS must be positive and at most 10m0s"},
		{name: "youtubejs startup zero", mutate: func(c *Config) { c.YouTubeJSStartupTimeout = 0 }, wantSub: "YOUTUBE_COLLECTOR_YOUTUBEJS_STARTUP_TIMEOUT_SECONDS must be positive"},
		{name: "youtubejs shutdown above 10m", mutate: func(c *Config) { c.YouTubeJSShutdownTimeout = time.Hour }, wantSub: "YOUTUBE_COLLECTOR_YOUTUBEJS_SHUTDOWN_TIMEOUT_SECONDS must be positive and at most 10m0s"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.mutate(&cfg)

			if err := cfg.Validate(holodex, official); err == nil || !strings.Contains(err.Error(), test.wantSub) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantSub)
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
			cfg := fixtureCollectorConfig(t)

			cfg.InstanceID = test.id

			if err := cfg.Validate(holodex, official); err == nil {
				t.Fatal("invalid production instance ID must fail closed")
			}
		})
	}

	cfg := fixtureCollectorConfig(t)

	cfg.InstanceID = collectorInstanceC

	if err := cfg.Validate(holodex, official); err != nil {
		t.Fatalf("valid production instance ID must be accepted: %v", err)
	}
}
