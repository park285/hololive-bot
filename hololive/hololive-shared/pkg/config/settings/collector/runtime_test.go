package collector

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/settingstest"
)

func setYouTubeCollectorRuntimeLoadEnv(t *testing.T) {
	t.Helper()
	settingstest.ClearIrisAndRoomEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("API_SECRET_KEY", "dummy-admin-secret")
	t.Setenv("METRICS_API_KEY", "dummy-metrics-secret")
	settingstest.SetRuntimeH3ServerEnv(t)
	t.Setenv("POSTGRES_USER", load.PostgresScraperRoleUser)
	t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", settingstest.CollectorInstanceC)
	t.Setenv("YOUTUBE_COLLECTOR_RUNTIME_ALLOWED", "true")
	t.Setenv("PHOTO_SYNC_ENABLED", "false")
	settingstest.UseProfileFixture(t, "stack-worker-profile-youtube-collector.json")
	t.Setenv("HOLODEX_API_KEY", "dummy-holodex")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("CLIPROXY_API_KEY", "")
	t.Setenv("EXA_API_KEY", "")

	for _, key := range []string{
		"CACHE_HOST",
		"CACHE_PORT",
		"CACHE_PASSWORD",
		"CACHE_SOCKET_PATH",
		"CACHE_DB",
		settingstest.IrisWebhookTokenEnv,
		settingstest.IrisBotTokenEnv,
		"KAKAO_ROOMS",
		"LLM_MODEL",
		"CLIPROXY_API_KEY",
		"EXA_API_KEY",
	} {
		t.Setenv(key, "")

		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

func writeReadablePostgresCA(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "postgres-ca.pem")
	if err := os.WriteFile(path, []byte("test-ca\n"), 0o600); err != nil {
		t.Fatalf("write postgres CA fixture: %v", err)
	}

	return path
}

func validYouTubeCollectorRuntimeConfig(t *testing.T) *RuntimeConfig {
	t.Helper()

	cert := writeReadablePostgresCA(t)
	collector := DefaultConfig()

	collector.InstanceID = settingstest.CollectorInstanceC

	return &RuntimeConfig{
		Environment: load.EnvironmentProduction,
		Version:     "test",
		Server: settings.ServerConfig{
			Port:           30025,
			APIKey:         "test-metrics-key",
			HTTPTransports: []string{"h3"},
			H3Addr:         ":30025",
			H3CertFile:     "/run/hololive-bot/certs/hololive-h3.crt",
			H3KeyFile:      settingstest.HololiveH3KeyPath,
		},
		Tracing: settings.TracingConfig{
			Enabled:    true,
			Endpoint:   "otel-collector:4317",
			Insecure:   true,
			SampleRate: 0.1,
		},
		Postgres: settings.PostgresConfig{
			User:        load.PostgresScraperRoleUser,
			Password:    "x",
			SSLMode:     load.PostgresSSLModeVerifyFull,
			SSLRootCert: cert,
		},
		RuntimeOwnership: RuntimeOwnershipConfig{
			RuntimeAllowed:         true,
			PhotoSyncEnabled:       false,
			NotificationEgressRole: load.NotificationEgressRoleOff,
		},
		WorkerProfile: mustLoadCollectorWorkerProfile(t),
		Collector:     collector,
		Holodex: HolodexConfig{
			BaseURL:   settings.DefaultHolodexOperationalConfig().BaseURL,
			APIKey:    "x",
			Transport: ProviderTransportConfig{Timeout: settings.DefaultHolodexOperationalConfig().Timeout},
		},
		OfficialSchedule: OfficialScheduleConfig{
			BaseURL:   settings.DefaultOfficialScheduleConfig().BaseURL,
			Transport: ProviderTransportConfig{Timeout: settings.DefaultOfficialScheduleConfig().Timeout},
		},
	}
}

func TestCFG001CollectorLoaderSucceedsWithoutCacheEnv(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)

	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if cfg.Postgres.User != load.PostgresScraperRoleUser {
		t.Fatalf("Postgres.User = %q, want %s", cfg.Postgres.User, load.PostgresScraperRoleUser)
	}

	if cfg.Collector.InstanceID != settingstest.CollectorInstanceC {
		t.Fatalf("InstanceID = %q, want youtube-collector-c", cfg.Collector.InstanceID)
	}
}

func TestCFG001CollectorLoaderUsesOnlyDedicatedMetricsKey(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)
	t.Setenv("API_SECRET_KEY", "admin-only-key")
	t.Setenv("METRICS_API_KEY", "collector-metrics-key")

	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if cfg.Server.APIKey != "collector-metrics-key" {
		t.Fatalf("Server.APIKey = %q, want dedicated metrics key", cfg.Server.APIKey)
	}
}

func TestCFG001LeftoverCacheEnvStillLoadsYouTubeCollectorRuntime(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)
	t.Setenv("CACHE_HOST", "valkey-cache")
	t.Setenv("CACHE_PORT", "6379")
	t.Setenv("CACHE_PASSWORD", "leftover-cache-password")
	t.Setenv("CACHE_SOCKET_PATH", "/var/run/valkey/valkey.sock")
	t.Setenv("CACHE_DB", "0")

	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v, want success with leftover CACHE_* env and no Valkey dial", err)
	}

	if cfg.Collector.InstanceID != settingstest.CollectorInstanceC {
		t.Fatalf("InstanceID = %q, want youtube-collector-c", cfg.Collector.InstanceID)
	}
}

func TestCFG001CollectorLoaderSourceOmitsCacheAndUnrelatedConstructors(t *testing.T) {
	t.Parallel()

	packageDir := os.DirFS(".")

	names, err := fs.Glob(packageDir, "*.go")
	if err != nil {
		t.Fatalf("list collector package sources: %v", err)
	}

	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		source, err := fs.ReadFile(packageDir, name)
		if err != nil {
			t.Fatalf("read collector source %s: %v", name, err)
		}

		text := string(source)

		for _, forbidden := range []string{
			"CACHE_",
			"API_SECRET_KEY",
			"IRIS_",
			"KAKAO_",
			"settings.LoadValkeyConfig",
			"settings.LoadLLMConfig",
			"settings.LoadCliproxyConfig",
			"settings.LoadGeminiConfig",
			"settings.LoadExaConfig",
			"ProvideCacheResources",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("collector loader %s must not reference %s", name, forbidden)
			}
		}
	}
}

func TestCFG002CollectorLoaderAllowsMissingIrisKakaoAndLLM(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)

	cfg, err := LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime() error = %v", err)
	}

	if cfg.WorkerProfile == nil || !cfg.WorkerProfile.Loaded.Profile.Workers["collection"].Executor.Enabled {
		t.Fatal("collection executor.enabled = false, want true")
	}
}

func TestCFG002CollectorLoaderIgnoresAccidentalIrisToken(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)
	t.Setenv(settingstest.IrisBotTokenEnv, "accidental-egress-token")
	t.Setenv("IRIS_BASE_URL", "http://iris.invalid")

	if _, err := LoadRuntime(); err != nil {
		t.Fatalf("LoadRuntime() error = %v, want nil without Iris worker profile fetch", err)
	}
}

type collectorRuntimeValidateCase struct {
	name    string
	mutate  func(*RuntimeConfig)
	wantSub string
}

func collectorRuntimeOwnershipValidateCases() []collectorRuntimeValidateCase {
	return []collectorRuntimeValidateCase{
		{
			name:    "runtime disabled",
			mutate:  func(c *RuntimeConfig) { c.RuntimeOwnership.RuntimeAllowed = false },
			wantSub: "YOUTUBE_COLLECTOR_RUNTIME_ALLOWED",
		},
		{
			name:    "photo sync enabled",
			mutate:  func(c *RuntimeConfig) { c.RuntimeOwnership.PhotoSyncEnabled = true },
			wantSub: "PHOTO_SYNC_ENABLED",
		},
		{
			name:    "notification egress not off",
			mutate:  func(c *RuntimeConfig) { c.RuntimeOwnership.NotificationEgressRole = "" },
			wantSub: "NOTIFICATION_EGRESS_ROLE",
		},
		{
			name:    "empty instance id",
			mutate:  func(c *RuntimeConfig) { c.Collector.InstanceID = "" },
			wantSub: "YOUTUBE_COLLECTOR_INSTANCE_ID",
		},
	}
}

func collectorRuntimeServerValidateCases() []collectorRuntimeValidateCase {
	return []collectorRuntimeValidateCase{
		{
			name:    "server port zero",
			mutate:  func(c *RuntimeConfig) { c.Server.Port = 0 },
			wantSub: "SERVER_PORT",
		},
		{
			name:    "metrics key empty",
			mutate:  func(c *RuntimeConfig) { c.Server.APIKey = "" },
			wantSub: "METRICS_API_KEY",
		},
		{
			name:    "tracing disabled",
			mutate:  func(c *RuntimeConfig) { c.Tracing.Enabled = false },
			wantSub: load.TracingYouTubeCollectorCEnabledEnv,
		},
		{
			name:    "h3 cert missing",
			mutate:  func(c *RuntimeConfig) { c.Server.H3CertFile = "" },
			wantSub: "HOLOLIVE_H3_CERT_FILE",
		},
	}
}

func collectorRuntimePostgresValidateCases() []collectorRuntimeValidateCase {
	return []collectorRuntimeValidateCase{
		{
			name:    "postgres user mismatch",
			mutate:  func(c *RuntimeConfig) { c.Postgres.User = load.PostgresRuntimeRoleUser },
			wantSub: "POSTGRES_USER",
		},
		{
			name:    "postgres password empty",
			mutate:  func(c *RuntimeConfig) { c.Postgres.Password = "" },
			wantSub: "POSTGRES_PASSWORD",
		},
		{
			name:    "postgres sslmode insecure",
			mutate:  func(c *RuntimeConfig) { c.Postgres.SSLMode = "require" },
			wantSub: "POSTGRES_SSLMODE",
		},
		{
			name:    "postgres sslrootcert empty",
			mutate:  func(c *RuntimeConfig) { c.Postgres.SSLRootCert = "" },
			wantSub: "POSTGRES_SSLROOTCERT",
		},
	}
}

func collectorRuntimeUpstreamValidateCases() []collectorRuntimeValidateCase {
	return []collectorRuntimeValidateCase{
		{
			name:    "holodex key empty",
			mutate:  func(c *RuntimeConfig) { c.Holodex.APIKey = "" },
			wantSub: "HOLODEX_API_KEY",
		},
		{
			name:    "holodex timeout zero",
			mutate:  func(c *RuntimeConfig) { c.Holodex.Transport.Timeout = 0 },
			wantSub: "HOLODEX_TIMEOUT_SECONDS must be positive",
		},
		{
			name:    "holodex timeout negative",
			mutate:  func(c *RuntimeConfig) { c.Holodex.Transport.Timeout = -time.Second },
			wantSub: "HOLODEX_TIMEOUT_SECONDS must be positive",
		},
		{
			name:    "official url invalid",
			mutate:  func(c *RuntimeConfig) { c.OfficialSchedule.BaseURL = "http://schedule.example" },
			wantSub: "OFFICIAL_SCHEDULE_BASE_URL",
		},
		{
			name:    "official schedule timeout zero",
			mutate:  func(c *RuntimeConfig) { c.OfficialSchedule.Transport.Timeout = 0 },
			wantSub: "OFFICIAL_SCHEDULE_TIMEOUT_SECONDS must be positive",
		},
		{
			name:    "official schedule timeout negative",
			mutate:  func(c *RuntimeConfig) { c.OfficialSchedule.Transport.Timeout = -time.Second },
			wantSub: "OFFICIAL_SCHEDULE_TIMEOUT_SECONDS must be positive",
		},
	}
}

func TestCFG003ProductionValidationExact(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	tests := slices.Concat(
		collectorRuntimeOwnershipValidateCases(),
		collectorRuntimeServerValidateCases(),
		collectorRuntimePostgresValidateCases(),
		collectorRuntimeUpstreamValidateCases(),
	)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validYouTubeCollectorRuntimeConfig(t)
			test.mutate(cfg)

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantSub) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantSub)
			}
		})
	}
}

func TestCFG003ProductionAcceptsCompleteConfig(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if cfg.Collector.QueueCapacity != cfg.Collector.TotalWorkers*4 {
		t.Fatalf("applied collector queue = %d, want %d", cfg.Collector.QueueCapacity, cfg.Collector.TotalWorkers*4)
	}
}

func TestCFG003ProductionRejectsMissingMetricsSecretEvenWithAdminSecret(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)
	t.Setenv("API_SECRET_KEY", "admin-only-key")

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Server.APIKey = ""

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "METRICS_API_KEY") {
		t.Fatalf("Validate() error = %v, want METRICS_API_KEY rejection", err)
	}
}

func TestCFG003ProductionRejectsUnreadableSSLRootCert(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Postgres.SSLRootCert = filepath.Join(t.TempDir(), "missing-ca.pem")

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "POSTGRES_SSLROOTCERT") {
		t.Fatalf("Validate() error = %v, want unreadable POSTGRES_SSLROOTCERT", err)
	}
}

func TestCFG004ProxyTruthTableAndUserinfoRedaction(t *testing.T) {
	t.Parallel()

	const redactionSentinel = "proxy-userinfo-value"

	tests := []struct {
		name    string
		proxy   ProxyConfig
		wantErr bool
		wantSub string
	}{
		{name: "direct mode", proxy: ProxyConfig{}},
		{name: "proxy mode", proxy: ProxyConfig{Enabled: true, URL: "http://127.0.0.1:8080"}},
		{name: "proxy with userinfo", proxy: ProxyConfig{Enabled: true, URL: "http://user:" + redactionSentinel + "@127.0.0.1:8080"}},
		{
			name:    "disabled with url",
			proxy:   ProxyConfig{Enabled: false, URL: "http://user:" + redactionSentinel + "@127.0.0.1:8080"},
			wantErr: true,
			wantSub: "SCRAPER_PROXY_URL must be empty",
		},
		{
			name:    "enabled without url",
			proxy:   ProxyConfig{Enabled: true},
			wantErr: true,
			wantSub: "SCRAPER_PROXY_URL is required",
		},
		{
			name:    "invalid scheme",
			proxy:   ProxyConfig{Enabled: true, URL: "socks5://user:" + redactionSentinel + "@127.0.0.1:1080"},
			wantErr: true,
			wantSub: "scheme",
		},
		{
			name:    "path not empty",
			proxy:   ProxyConfig{Enabled: true, URL: "http://127.0.0.1:8080/proxy"},
			wantErr: true,
			wantSub: "path",
		},
		{
			name:    "query forbidden",
			proxy:   ProxyConfig{Enabled: true, URL: "http://127.0.0.1:8080?x=1"},
			wantErr: true,
			wantSub: "query or fragment",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.proxy.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, test.wantErr)
			}

			if err != nil && test.wantSub != "" && !strings.Contains(err.Error(), test.wantSub) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantSub)
			}

			if err != nil && strings.Contains(err.Error(), redactionSentinel) {
				t.Fatalf("proxy validation leaked userinfo: %v", err)
			}
		})
	}
}

func TestValidateYouTubeCollectorRuntimeDoesNotDefaultCollectorConfig(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Collector.TotalWorkers = 8
	cfg.Collector.QueueCapacity = 0
	cfg.Collector.AcquisitionBatch = 0
	cfg.Collector.HolodexMaxInflight = 0
	cfg.Collector.OfficialMaxInflight = 0
	cfg.Collector.YouTubeJSMaxInflight = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want zero worker settings rejection")
	}

	if cfg.Collector.QueueCapacity != 0 || cfg.Collector.AcquisitionBatch != 0 || cfg.Collector.HolodexMaxInflight != 0 {
		t.Fatalf("collector settings were defaulted: %#v", cfg.Collector)
	}
}

func TestValidateYouTubeCollectorRuntimeRejectsInvalidCollectorBudget(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Collector.RenewInterval = 59 * time.Second

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "renew timing") {
		t.Fatalf("Validate() error = %v, want renew timing rejection", err)
	}
}

func TestValidateYouTubeCollectorRuntimeRequiresScraperPostgresUser(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Postgres.User = load.PostgresRuntimeRoleUser

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "POSTGRES_USER="+load.PostgresScraperRoleUser) {
		t.Fatalf("Validate() error = %v, want scraper postgres user", err)
	}
}

func TestValidateYouTubeCollectorRuntimeRejectsMissingHolodexAPIKey(t *testing.T) {
	settingstest.ClearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Holodex.APIKey = ""

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY") {
		t.Fatalf("Validate() error = %v, want Holodex key requirement", err)
	}
}
