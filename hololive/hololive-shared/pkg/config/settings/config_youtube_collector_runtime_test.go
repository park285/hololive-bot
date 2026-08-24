package settings

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func setYouTubeCollectorRuntimeLoadEnv(t *testing.T) {
	t.Helper()
	clearIrisAndRoomEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("API_SECRET_KEY", "dummy-secret")
	setRuntimeH3ServerEnv(t)
	t.Setenv("POSTGRES_USER", postgresScraperRoleUser)
	t.Setenv("YOUTUBE_COLLECTOR_INSTANCE_ID", collectorInstanceC)
	t.Setenv("YOUTUBE_COLLECTOR_RUNTIME_ALLOWED", "true")
	t.Setenv("PHOTO_SYNC_ENABLED", "false")
	useStackWorkerProfileFixture(t, "stack-worker-profile-youtube-collector.json")
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
		irisWebhookTokenEnv,
		irisBotTokenEnv,
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

func validYouTubeCollectorRuntimeConfig(t *testing.T) *YouTubeCollectorRuntimeConfig {
	t.Helper()

	cert := writeReadablePostgresCA(t)
	collector := DefaultYouTubeCollectorConfig()

	collector.InstanceID = collectorInstanceC

	return &YouTubeCollectorRuntimeConfig{
		Environment: environmentProduction,
		Version:     "test",
		Server: ServerConfig{
			Port:           30025,
			APIKey:         "test-api-key",
			HTTPTransports: []string{"h3"},
			H3Addr:         ":30025",
			H3CertFile:     "/run/hololive-bot/certs/hololive-h3.crt",
			H3KeyFile:      hololiveH3KeyPath,
		},
		Tracing: TracingConfig{
			Enabled:    true,
			Endpoint:   "otel-collector:4317",
			Insecure:   true,
			SampleRate: defaultOTELSampleRate,
		},
		Postgres: PostgresConfig{
			User:        postgresScraperRoleUser,
			Password:    "x",
			SSLMode:     postgresSSLModeVerifyFull,
			SSLRootCert: cert,
		},
		RuntimeOwnership: CollectorRuntimeOwnershipConfig{
			RuntimeAllowed:         true,
			PhotoSyncEnabled:       false,
			NotificationEgressRole: notificationEgressRoleOff,
		},
		WorkerProfile: mustLoadCollectorWorkerProfile(t),
		Collector:     collector,
		Holodex: CollectorHolodexConfig{
			BaseURL:   DefaultHolodexOperationalConfig().BaseURL,
			APIKey:    "x",
			Transport: ProviderTransportConfig{Timeout: DefaultHolodexOperationalConfig().Timeout},
		},
		OfficialSchedule: CollectorOfficialScheduleConfig{
			BaseURL:   DefaultOfficialScheduleConfig().BaseURL,
			Transport: ProviderTransportConfig{Timeout: DefaultOfficialScheduleConfig().Timeout},
		},
	}
}

func TestCFG001CollectorLoaderSucceedsWithoutCacheEnv(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)

	cfg, err := LoadYouTubeCollectorRuntime()
	if err != nil {
		t.Fatalf("LoadYouTubeCollectorRuntime() error = %v", err)
	}

	if cfg.Postgres.User != postgresScraperRoleUser {
		t.Fatalf("Postgres.User = %q, want %s", cfg.Postgres.User, postgresScraperRoleUser)
	}

	if cfg.Collector.InstanceID != collectorInstanceC {
		t.Fatalf("InstanceID = %q, want youtube-collector-c", cfg.Collector.InstanceID)
	}
}

func TestCFG001LeftoverCacheEnvStillLoadsYouTubeCollectorRuntime(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)
	t.Setenv("CACHE_HOST", "valkey-cache")
	t.Setenv("CACHE_PORT", "6379")
	t.Setenv("CACHE_PASSWORD", "leftover-cache-password")
	t.Setenv("CACHE_SOCKET_PATH", "/var/run/valkey/valkey.sock")
	t.Setenv("CACHE_DB", "0")

	cfg, err := LoadYouTubeCollectorRuntime()
	if err != nil {
		t.Fatalf("LoadYouTubeCollectorRuntime() error = %v, want success with leftover CACHE_* env and no Valkey dial", err)
	}

	if cfg.Collector.InstanceID != collectorInstanceC {
		t.Fatalf("InstanceID = %q, want youtube-collector-c", cfg.Collector.InstanceID)
	}
}

func TestCFG001CollectorLoaderSourceOmitsCacheAndUnrelatedConstructors(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("config_youtube_collector_runtime.go")
	if err != nil {
		t.Fatalf("read collector runtime loader: %v", err)
	}

	text := string(source)

	for _, forbidden := range []string{
		"loadValkeyConfig",
		"loadIrisConfig",
		"loadKakaoConfig",
		"loadLLMConfig",
		"loadCliproxyConfig",
		"loadExaConfig",
		"loadChzzkConfig",
		"loadTwitchConfig",
		"loadNotificationConfig",
		"loadCORSConfig",
		"loadScraperConfig",
		"loadYouTubeConfig",
		"ProvideCacheResources",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("collector loader must not call %s", forbidden)
		}
	}
}

func TestCFG002CollectorLoaderAllowsMissingIrisKakaoAndLLM(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)

	cfg, err := LoadYouTubeCollectorRuntime()
	if err != nil {
		t.Fatalf("LoadYouTubeCollectorRuntime() error = %v", err)
	}

	if cfg.WorkerProfile == nil || !cfg.WorkerProfile.Loaded.Profile.Workers["collection"].Executor.Enabled {
		t.Fatal("collection executor.enabled = false, want true")
	}
}

func TestCFG002CollectorLoaderIgnoresAccidentalIrisToken(t *testing.T) {
	setYouTubeCollectorRuntimeLoadEnv(t)
	t.Setenv(irisBotTokenEnv, "accidental-egress-token")
	t.Setenv("IRIS_BASE_URL", "http://iris.invalid")

	if _, err := LoadYouTubeCollectorRuntime(); err != nil {
		t.Fatalf("LoadYouTubeCollectorRuntime() error = %v, want nil without Iris worker profile fetch", err)
	}
}

type collectorRuntimeValidateCase struct {
	name    string
	mutate  func(*YouTubeCollectorRuntimeConfig)
	wantSub string
}

func collectorRuntimeOwnershipValidateCases() []collectorRuntimeValidateCase {
	return []collectorRuntimeValidateCase{
		{
			name:    "runtime disabled",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.RuntimeOwnership.RuntimeAllowed = false },
			wantSub: "YOUTUBE_COLLECTOR_RUNTIME_ALLOWED",
		},
		{
			name:    "photo sync enabled",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.RuntimeOwnership.PhotoSyncEnabled = true },
			wantSub: "PHOTO_SYNC_ENABLED",
		},
		{
			name:    "notification egress not off",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.RuntimeOwnership.NotificationEgressRole = "" },
			wantSub: "NOTIFICATION_EGRESS_ROLE",
		},
		{
			name:    "empty instance id",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Collector.InstanceID = "" },
			wantSub: "YOUTUBE_COLLECTOR_INSTANCE_ID",
		},
	}
}

func collectorRuntimeServerValidateCases() []collectorRuntimeValidateCase {
	return []collectorRuntimeValidateCase{
		{
			name:    "server port zero",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Server.Port = 0 },
			wantSub: "SERVER_PORT",
		},
		{
			name:    "api key empty",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Server.APIKey = "" },
			wantSub: "API_SECRET_KEY",
		},
		{
			name:    "tracing disabled",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Tracing.Enabled = false },
			wantSub: tracingYouTubeCollectorCEnabledEnv,
		},
		{
			name:    "h3 cert missing",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Server.H3CertFile = "" },
			wantSub: "HOLOLIVE_H3_CERT_FILE",
		},
	}
}

func collectorRuntimePostgresValidateCases() []collectorRuntimeValidateCase {
	return []collectorRuntimeValidateCase{
		{
			name:    "postgres user mismatch",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Postgres.User = postgresRuntimeRoleUser },
			wantSub: "POSTGRES_USER",
		},
		{
			name:    "postgres password empty",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Postgres.Password = "" },
			wantSub: "POSTGRES_PASSWORD",
		},
		{
			name:    "postgres sslmode insecure",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Postgres.SSLMode = "require" },
			wantSub: "POSTGRES_SSLMODE",
		},
		{
			name:    "postgres sslrootcert empty",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Postgres.SSLRootCert = "" },
			wantSub: "POSTGRES_SSLROOTCERT",
		},
	}
}

func collectorRuntimeUpstreamValidateCases() []collectorRuntimeValidateCase {
	return []collectorRuntimeValidateCase{
		{
			name:    "holodex key empty",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Holodex.APIKey = "" },
			wantSub: "HOLODEX_API_KEY",
		},
		{
			name:    "holodex timeout zero",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Holodex.Transport.Timeout = 0 },
			wantSub: "HOLODEX_TIMEOUT_SECONDS must be positive",
		},
		{
			name:    "holodex timeout negative",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.Holodex.Transport.Timeout = -time.Second },
			wantSub: "HOLODEX_TIMEOUT_SECONDS must be positive",
		},
		{
			name:    "official url invalid",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.OfficialSchedule.BaseURL = "http://schedule.example" },
			wantSub: "OFFICIAL_SCHEDULE_BASE_URL",
		},
		{
			name:    "official schedule timeout zero",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.OfficialSchedule.Transport.Timeout = 0 },
			wantSub: "OFFICIAL_SCHEDULE_TIMEOUT_SECONDS must be positive",
		},
		{
			name:    "official schedule timeout negative",
			mutate:  func(c *YouTubeCollectorRuntimeConfig) { c.OfficialSchedule.Transport.Timeout = -time.Second },
			wantSub: "OFFICIAL_SCHEDULE_TIMEOUT_SECONDS must be positive",
		},
	}
}

func TestCFG003ProductionValidationExact(t *testing.T) {
	clearRuntimeRoleEnv(t)

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
	clearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if cfg.Collector.QueueCapacity != cfg.Collector.TotalWorkers*4 {
		t.Fatalf("applied collector queue = %d, want %d", cfg.Collector.QueueCapacity, cfg.Collector.TotalWorkers*4)
	}
}

func TestCFG003ProductionRejectsMissingAPISecret(t *testing.T) {
	clearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Server.APIKey = ""

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "API_SECRET_KEY") {
		t.Fatalf("Validate() error = %v, want API_SECRET_KEY rejection", err)
	}
}

func TestCFG003ProductionRejectsUnreadableSSLRootCert(t *testing.T) {
	clearRuntimeRoleEnv(t)

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
		proxy   CollectorProxyConfig
		wantErr bool
		wantSub string
	}{
		{name: "direct mode", proxy: CollectorProxyConfig{}},
		{name: "proxy mode", proxy: CollectorProxyConfig{Enabled: true, URL: "http://127.0.0.1:8080"}},
		{name: "proxy with userinfo", proxy: CollectorProxyConfig{Enabled: true, URL: "http://user:" + redactionSentinel + "@127.0.0.1:8080"}},
		{
			name:    "disabled with url",
			proxy:   CollectorProxyConfig{Enabled: false, URL: "http://user:" + redactionSentinel + "@127.0.0.1:8080"},
			wantErr: true,
			wantSub: "SCRAPER_PROXY_URL must be empty",
		},
		{
			name:    "enabled without url",
			proxy:   CollectorProxyConfig{Enabled: true},
			wantErr: true,
			wantSub: "SCRAPER_PROXY_URL is required",
		},
		{
			name:    "invalid scheme",
			proxy:   CollectorProxyConfig{Enabled: true, URL: "socks5://user:" + redactionSentinel + "@127.0.0.1:1080"},
			wantErr: true,
			wantSub: "scheme",
		},
		{
			name:    "path not empty",
			proxy:   CollectorProxyConfig{Enabled: true, URL: "http://127.0.0.1:8080/proxy"},
			wantErr: true,
			wantSub: "path",
		},
		{
			name:    "query forbidden",
			proxy:   CollectorProxyConfig{Enabled: true, URL: "http://127.0.0.1:8080?x=1"},
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
	clearRuntimeRoleEnv(t)

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
	clearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Collector.RenewInterval = 59 * time.Second

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "renew timing") {
		t.Fatalf("Validate() error = %v, want renew timing rejection", err)
	}
}

func TestValidateYouTubeCollectorRuntimeRequiresScraperPostgresUser(t *testing.T) {
	clearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Postgres.User = postgresRuntimeRoleUser

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "POSTGRES_USER="+postgresScraperRoleUser) {
		t.Fatalf("Validate() error = %v, want scraper postgres user", err)
	}
}

func TestValidateYouTubeCollectorRuntimeRejectsMissingHolodexAPIKey(t *testing.T) {
	clearRuntimeRoleEnv(t)

	cfg := validYouTubeCollectorRuntimeConfig(t)

	cfg.Holodex.APIKey = ""

	err := cfg.Validate()

	if err == nil || !strings.Contains(err.Error(), "HOLODEX_API_KEY") {
		t.Fatalf("Validate() error = %v, want Holodex key requirement", err)
	}
}
