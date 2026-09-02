package collector

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

type RuntimeConfig struct {
	Environment string
	Version     string

	Server   settings.ServerConfig
	Logging  settings.LoggingConfig
	Tracing  settings.TracingConfig
	Postgres settings.PostgresConfig

	RuntimeOwnership RuntimeOwnershipConfig
	WorkerProfile    *settings.YouTubeCollectorWorkerProfile
	Collector        Config
	Proxy            ProxyConfig
	Holodex          HolodexConfig
	OfficialSchedule OfficialScheduleConfig
}

type RuntimeOwnershipConfig struct {
	RuntimeAllowed         bool
	PhotoSyncEnabled       bool
	NotificationEgressRole string
}

type ProxyConfig struct {
	Enabled bool
	URL     string
}

type HolodexConfig struct {
	BaseURL   string
	APIKey    string
	Transport ProviderTransportConfig
}

type OfficialScheduleConfig struct {
	BaseURL   string
	Transport ProviderTransportConfig
}

type ProviderTransportConfig struct {
	Timeout time.Duration
}

func LoadRuntime() (*RuntimeConfig, error) {
	if err := load.DotEnv(); err != nil {
		return nil, fmt.Errorf("load youtube collector runtime: %w", err)
	}

	config, err := buildRuntimeConfig()
	if err != nil {
		return nil, fmt.Errorf("load youtube collector runtime: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

func buildRuntimeConfig() (*RuntimeConfig, error) {
	workerProfile, err := LoadWorkerProfile()
	if err != nil {
		return nil, fmt.Errorf("load youtube collector worker profile: %w", err)
	}

	collector, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("load youtube collector config: %w", err)
	}

	tracingConfig, err := settings.LoadTracingConfig(settings.TracingRuntimeYouTubeCollector, collector.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("load tracing config: %w", err)
	}

	ownership, err := loadRuntimeOwnershipConfig()
	if err != nil {
		return nil, fmt.Errorf("load collector runtime ownership config: %w", err)
	}

	proxy, err := loadProxyConfig()
	if err != nil {
		return nil, fmt.Errorf("load collector proxy config: %w", err)
	}

	config := &RuntimeConfig{
		Environment:      load.AppEnvironment(),
		Version:          sharedenv.String("APP_VERSION", "1.1.0-go"),
		Server:           settings.LoadServerConfigWithAPIKey(sharedenv.String("METRICS_API_KEY", "")),
		Logging:          settings.LoadLoggingConfig(),
		Tracing:          tracingConfig,
		Postgres:         settings.LoadPostgresConfig(),
		RuntimeOwnership: ownership,
		WorkerProfile:    workerProfile,
		Collector:        collector,
		Proxy:            proxy,
		Holodex:          loadHolodexConfig(),
		OfficialSchedule: loadOfficialScheduleConfig(),
	}
	applyWorkerProfile(config)

	return config, nil
}

func loadRuntimeOwnershipConfig() (RuntimeOwnershipConfig, error) {
	runtimeAllowed, err := sharedenv.BoolE("YOUTUBE_COLLECTOR_RUNTIME_ALLOWED", false)
	if err != nil {
		return RuntimeOwnershipConfig{}, fmt.Errorf("read bool env: %w", err)
	}

	photoSyncEnabled, err := sharedenv.BoolE("PHOTO_SYNC_ENABLED", false)
	if err != nil {
		return RuntimeOwnershipConfig{}, fmt.Errorf("read bool env: %w", err)
	}

	return RuntimeOwnershipConfig{
		RuntimeAllowed:         runtimeAllowed,
		PhotoSyncEnabled:       photoSyncEnabled,
		NotificationEgressRole: load.TrimmedEnv(load.NotificationEgressRoleEnv),
	}, nil
}

func loadProxyConfig() (ProxyConfig, error) {
	enabled, err := sharedenv.BoolE("SCRAPER_PROXY_ENABLED", false)
	if err != nil {
		return ProxyConfig{}, fmt.Errorf("read bool env: %w", err)
	}

	return ProxyConfig{
		Enabled: enabled,
		URL:     strings.TrimSpace(sharedenv.String("SCRAPER_PROXY_URL", "")),
	}, nil
}

func loadHolodexConfig() HolodexConfig {
	defaults := settings.DefaultHolodexOperationalConfig()

	return HolodexConfig{
		BaseURL: sharedenv.String("HOLODEX_BASE_URL", defaults.BaseURL),
		APIKey:  load.HolodexAPIKey(),
		Transport: ProviderTransportConfig{
			Timeout: time.Duration(sharedenv.Int("HOLODEX_TIMEOUT_SECONDS", int(defaults.Timeout/time.Second))) * time.Second,
		},
	}
}

func loadOfficialScheduleConfig() OfficialScheduleConfig {
	defaults := settings.DefaultOfficialScheduleConfig()

	return OfficialScheduleConfig{
		BaseURL: sharedenv.String("OFFICIAL_SCHEDULE_BASE_URL", defaults.BaseURL),
		Transport: ProviderTransportConfig{
			Timeout: time.Duration(sharedenv.Int("OFFICIAL_SCHEDULE_TIMEOUT_SECONDS", int(defaults.Timeout/time.Second))) * time.Second,
		},
	}
}

func (c *RuntimeConfig) Validate() error {
	if c == nil {
		return errors.New("youtube collector runtime config is nil")
	}

	if err := load.ValidateUnsupportedLegacyEnvUsage(); err != nil {
		return fmt.Errorf("validate unsupported legacy env usage: %w", err)
	}

	if err := c.validateServer(); err != nil {
		return fmt.Errorf("validate server: %w", err)
	}

	if err := c.validateTracing(); err != nil {
		return fmt.Errorf("validate tracing: %w", err)
	}

	if err := c.validateOwnership(); err != nil {
		return fmt.Errorf("validate ownership: %w", err)
	}

	return errors.Join(c.validateRuntimeDependencies())
}

func (c *RuntimeConfig) validateRuntimeDependencies() error {
	if err := c.validatePostgres(); err != nil {
		return fmt.Errorf("validate postgres: %w", err)
	}

	if err := c.Proxy.Validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if err := c.validateProviders(); err != nil {
		return fmt.Errorf("validate providers: %w", err)
	}

	if err := c.applyCollector(); err != nil {
		return fmt.Errorf("apply collector: %w", err)
	}

	return nil
}

func (c *RuntimeConfig) validateServer() error {
	if c.Server.Port <= 0 {
		return errors.New("SERVER_PORT is required")
	}

	if err := settings.ValidateServerTransports(&c.Server); err != nil {
		return fmt.Errorf("validate server transports: %w", err)
	}

	if err := validateCollectorMetricsAPIKey(c.Environment, c.Server.APIKey); err != nil {
		return fmt.Errorf("validate metrics API key: %w", err)
	}

	return nil
}

func validateCollectorMetricsAPIKey(environment, apiKey string) error {
	if !load.IsProduction(environment) || strings.TrimSpace(apiKey) != "" {
		return nil
	}

	return errors.New("METRICS_API_KEY is required in production")
}

func (c *RuntimeConfig) validateTracing() error {
	if err := settings.ValidateTracingConfig(c.Tracing); err != nil {
		return fmt.Errorf("validate tracing config: %w", err)
	}

	if !load.IsProduction(c.Environment) || c.Tracing.Enabled {
		return nil
	}

	enabledEnv, err := settings.YouTubeCollectorTracingEnabledEnv(c.Collector.InstanceID)
	if err != nil {
		return fmt.Errorf("youtube collector tracing enabled env: %w", err)
	}

	return fmt.Errorf("%s=true is required in production", enabledEnv)
}

func (c *RuntimeConfig) validateOwnership() error {
	if err := c.validateCollectorOwnershipFlags(); err != nil {
		return fmt.Errorf("validate collector ownership flags: %w", err)
	}

	if err := c.validateCollectorEgressOwnership(); err != nil {
		return fmt.Errorf("validate collector egress ownership: %w", err)
	}

	if err := c.validateProductionCollectorOwnership(); err != nil {
		return fmt.Errorf("validate production collector ownership: %w", err)
	}

	return nil
}

func (c *RuntimeConfig) validateCollectorOwnershipFlags() error {
	if !c.RuntimeOwnership.RuntimeAllowed {
		return errors.New("youtube collector runtime disabled: set YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true on the owning host")
	}

	if c.WorkerProfile == nil {
		return errors.New("youtube collector worker profile is required")
	}

	if c.RuntimeOwnership.PhotoSyncEnabled {
		return fmt.Errorf("%s requires PHOTO_SYNC_ENABLED=false", load.RuntimeYouTubeCollector)
	}

	return nil
}

func (c *RuntimeConfig) validateCollectorEgressOwnership() error {
	if err := load.ValidateNotificationRoleEnvValues(); err != nil {
		return fmt.Errorf("validate notification role env values: %w", err)
	}

	if err := load.RejectReservedEgressRoles(load.RuntimeYouTubeCollector); err != nil {
		return fmt.Errorf("reject reserved egress roles: %w", err)
	}

	return nil
}

func (c *RuntimeConfig) validateProductionCollectorOwnership() error {
	if !load.IsProduction(c.Environment) {
		return nil
	}

	if !strings.EqualFold(c.RuntimeOwnership.NotificationEgressRole, load.NotificationEgressRoleOff) {
		return fmt.Errorf("%s production requires %s=%s", load.RuntimeYouTubeCollector, load.NotificationEgressRoleEnv, load.NotificationEgressRoleOff)
	}

	return nil
}

func (c *RuntimeConfig) validatePostgres() error {
	if err := validatePostgresUser(c.Postgres.User); err != nil {
		return fmt.Errorf("validate youtube collector postgres user: %w", err)
	}

	if err := load.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if !load.IsProduction(c.Environment) {
		return nil
	}

	if strings.TrimSpace(c.Postgres.Password) == "" {
		return fmt.Errorf("%s production requires POSTGRES_PASSWORD", load.RuntimeYouTubeCollector)
	}

	if err := validateReadablePostgresSSLRootCert(c.Postgres.SSLRootCert); err != nil {
		return fmt.Errorf("validate readable postgres SSL root cert: %w", err)
	}

	return nil
}

func (c *RuntimeConfig) validateProviders() error {
	if err := load.ValidateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	if err := load.ValidateHolodexTimeout(c.Holodex.Transport.Timeout); err != nil {
		return fmt.Errorf("validate holodex timeout: %w", err)
	}

	if err := load.ValidateOfficialScheduleBaseURL(c.OfficialSchedule.BaseURL); err != nil {
		return fmt.Errorf("validate official schedule base URL: %w", err)
	}

	if err := load.ValidateOfficialScheduleTimeout(c.OfficialSchedule.Transport.Timeout); err != nil {
		return fmt.Errorf("validate official schedule timeout: %w", err)
	}

	return nil
}

func (c *RuntimeConfig) applyCollector() error {
	if err := c.Collector.Validate(c.Holodex.Transport.Timeout, c.OfficialSchedule.Transport.Timeout); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	return nil
}

func applyWorkerProfile(config *RuntimeConfig) {
	profile := config.WorkerProfile
	worker := profile.Loaded.Profile.Workers["collection"]
	collection := profile.Collection

	config.Collector.TotalWorkers = worker.Executor.ConfiguredWorkers
	config.Collector.QueueCapacity = int(*worker.Queue.Capacity.Items)
	config.Collector.AcquisitionBatch = collection.AcquisitionBatch
	config.Collector.AcquisitionCadence = time.Duration(collection.AcquisitionCadenceMS) * time.Millisecond
	config.Collector.LeaseTTL = time.Duration(collection.LeaseTTLMS) * time.Millisecond
	config.Collector.RenewInterval = time.Duration(collection.RenewIntervalMS) * time.Millisecond
	config.Collector.RenewTimeout = time.Duration(collection.RenewTimeoutMS) * time.Millisecond
	config.Collector.DBTimeout = time.Duration(collection.DBTimeoutMS) * time.Millisecond
	config.Collector.CleanupTimeout = time.Duration(collection.CleanupTimeoutMS) * time.Millisecond
	config.Collector.ProviderAdmissionTimeout = time.Duration(collection.ProviderAdmissionTimeoutMS) * time.Millisecond
	config.Collector.CollectionOverhead = time.Duration(collection.CollectionOverheadMS) * time.Millisecond
	config.Collector.PublishTimeout = time.Duration(collection.PublishTimeoutMS) * time.Millisecond
	config.Collector.RetryMin = time.Duration(collection.RetryMinMS) * time.Millisecond
	config.Collector.RetryMax = time.Duration(collection.RetryMaxMS) * time.Millisecond
	config.Collector.ReleaseJitterMin = time.Duration(collection.ReleaseJitterMinMS) * time.Millisecond
	config.Collector.ReleaseJitterMax = time.Duration(collection.ReleaseJitterMaxMS) * time.Millisecond
	config.Collector.HolodexMaxInflight = collection.HolodexMaxInflight
	config.Collector.OfficialMaxInflight = collection.OfficialMaxInflight
	config.Collector.YouTubeJSMaxInflight = collection.YouTubeJSMaxInflight
}

func (c ProxyConfig) Validate() error {
	if !c.Enabled {
		if c.URL == "" {
			return nil
		}

		return fmt.Errorf("SCRAPER_PROXY_URL must be empty when SCRAPER_PROXY_ENABLED=false (got %s)", redactedProxyURL(c.URL))
	}

	if c.URL == "" {
		return errors.New("SCRAPER_PROXY_URL is required when SCRAPER_PROXY_ENABLED=true")
	}

	if err := validateCollectorProxyURL(c.URL); err != nil {
		return fmt.Errorf("validate collector proxy URL: %w", err)
	}

	return nil
}

func validateCollectorProxyURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return errors.New("SCRAPER_PROXY_URL is invalid")
	}

	if err := validateCollectorProxyEndpoint(parsed, raw); err != nil {
		return fmt.Errorf("validate collector proxy endpoint: %w", err)
	}

	if err := validateCollectorProxyResource(parsed, raw); err != nil {
		return fmt.Errorf("validate collector proxy resource: %w", err)
	}

	return nil
}

func validateCollectorProxyEndpoint(parsed *url.URL, raw string) error {
	if parsed.Scheme != load.SchemeHTTP && parsed.Scheme != load.SchemeHTTPS {
		return fmt.Errorf("SCRAPER_PROXY_URL scheme must be http or https (got %s)", redactedProxyURL(raw))
	}

	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("SCRAPER_PROXY_URL host must be non-empty (got %s)", redactedProxyURL(raw))
	}

	return nil
}

func validateCollectorProxyResource(parsed *url.URL, raw string) error {
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("SCRAPER_PROXY_URL path must be empty or / (got %s)", redactedProxyURL(raw))
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("SCRAPER_PROXY_URL must not contain query or fragment (got %s)", redactedProxyURL(raw))
	}

	return nil
}

func redactedProxyURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return "<invalid-proxy-url>"
	}

	if parsed.User != nil {
		parsed.User = url.User("redacted")
	}

	return parsed.String()
}

func validateReadablePostgresSSLRootCert(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("POSTGRES_SSLROOTCERT is required in production")
	}

	file, err := os.Open(trimmed) //nolint:gosec // 운영자가 지정한 CA 경로의 읽기 가능성을 기동 시 검증해야 합니다.
	if err != nil {
		return fmt.Errorf("POSTGRES_SSLROOTCERT is not readable: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("POSTGRES_SSLROOTCERT close: %w", err)
	}

	return nil
}

// validatePostgresUser: collector는 scraper 역할 DB 사용자로만 접속한다.
func validatePostgresUser(user string) error {
	want := resolvedHololiveScraperUser()
	if strings.TrimSpace(user) != want {
		return fmt.Errorf("%s requires POSTGRES_USER=%s", load.RuntimeYouTubeCollector, want)
	}

	return nil
}

func resolvedHololiveScraperUser() string {
	user := strings.TrimSpace(sharedenv.String("HOLOLIVE_SCRAPER_USER", load.PostgresScraperRoleUser))
	if user == "" {
		return load.PostgresScraperRoleUser
	}

	return user
}
