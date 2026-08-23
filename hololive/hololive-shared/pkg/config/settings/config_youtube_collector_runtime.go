package settings

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

type YouTubeCollectorRuntimeConfig struct {
	Environment string
	Version     string

	Server   ServerConfig
	Logging  LoggingConfig
	Tracing  TracingConfig
	Postgres PostgresConfig

	RuntimeOwnership CollectorRuntimeOwnershipConfig
	WorkerProfile    *YouTubeCollectorWorkerProfile
	Collector        YouTubeCollectorConfig
	Proxy            CollectorProxyConfig
	Holodex          CollectorHolodexConfig
	OfficialSchedule CollectorOfficialScheduleConfig
}

type CollectorRuntimeOwnershipConfig struct {
	RuntimeAllowed         bool
	PhotoSyncEnabled       bool
	NotificationEgressRole string
}

type CollectorProxyConfig struct {
	Enabled bool
	URL     string
}

type CollectorHolodexConfig struct {
	BaseURL   string
	APIKey    string
	Transport ProviderTransportConfig
}

type CollectorOfficialScheduleConfig struct {
	BaseURL   string
	Transport ProviderTransportConfig
}

type ProviderTransportConfig struct {
	Timeout time.Duration
}

func LoadYouTubeCollectorRuntime() (*YouTubeCollectorRuntimeConfig, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load youtube collector runtime: load .env: %w", err)
	}
	config, err := buildYouTubeCollectorRuntimeConfig()
	if err != nil {
		return nil, fmt.Errorf("load youtube collector runtime: %w", err)
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}
	return config, nil
}

func buildYouTubeCollectorRuntimeConfig() (*YouTubeCollectorRuntimeConfig, error) {
	workerProfile, err := LoadYouTubeCollectorWorkerProfile()
	if err != nil {
		return nil, err
	}
	collector, err := loadYouTubeCollectorConfig()
	if err != nil {
		return nil, err
	}
	tracingConfig, err := loadTracingConfig(tracingRuntimeYouTubeCollector, collector.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("load tracing config: %w", err)
	}
	ownership, err := loadCollectorRuntimeOwnershipConfig()
	if err != nil {
		return nil, err
	}
	proxy, err := loadCollectorProxyConfig()
	if err != nil {
		return nil, err
	}
	config := &YouTubeCollectorRuntimeConfig{
		Environment:      loadAppEnvironment(),
		Version:          sharedenv.String("APP_VERSION", "1.1.0-go"),
		Server:           loadServerConfig(),
		Logging:          loadLoggingConfig(),
		Tracing:          tracingConfig,
		Postgres:         loadPostgresConfig(),
		RuntimeOwnership: ownership,
		WorkerProfile:    workerProfile,
		Collector:        collector,
		Proxy:            proxy,
		Holodex:          loadCollectorHolodexConfig(),
		OfficialSchedule: loadCollectorOfficialScheduleConfig(),
	}
	applyYouTubeCollectorWorkerProfile(config)
	return config, nil
}

func loadCollectorRuntimeOwnershipConfig() (CollectorRuntimeOwnershipConfig, error) {
	runtimeAllowed, err := sharedenv.BoolE("YOUTUBE_COLLECTOR_RUNTIME_ALLOWED", false)
	if err != nil {
		return CollectorRuntimeOwnershipConfig{}, err
	}
	photoSyncEnabled, err := sharedenv.BoolE("PHOTO_SYNC_ENABLED", false)
	if err != nil {
		return CollectorRuntimeOwnershipConfig{}, err
	}
	return CollectorRuntimeOwnershipConfig{
		RuntimeAllowed:         runtimeAllowed,
		PhotoSyncEnabled:       photoSyncEnabled,
		NotificationEgressRole: trimmedEnv(notificationEgressRoleEnv),
	}, nil
}

func loadCollectorProxyConfig() (CollectorProxyConfig, error) {
	enabled, err := sharedenv.BoolE("SCRAPER_PROXY_ENABLED", false)
	if err != nil {
		return CollectorProxyConfig{}, err
	}
	return CollectorProxyConfig{
		Enabled: enabled,
		URL:     strings.TrimSpace(sharedenv.String("SCRAPER_PROXY_URL", "")),
	}, nil
}

func loadCollectorHolodexConfig() CollectorHolodexConfig {
	defaults := DefaultHolodexOperationalConfig()
	return CollectorHolodexConfig{
		BaseURL: sharedenv.String("HOLODEX_BASE_URL", defaults.BaseURL),
		APIKey:  resolveHolodexAPIKey(),
		Transport: ProviderTransportConfig{
			Timeout: time.Duration(sharedenv.Int("HOLODEX_TIMEOUT_SECONDS", int(defaults.Timeout/time.Second))) * time.Second,
		},
	}
}

func loadCollectorOfficialScheduleConfig() CollectorOfficialScheduleConfig {
	defaults := DefaultOfficialScheduleConfig()
	return CollectorOfficialScheduleConfig{
		BaseURL: sharedenv.String("OFFICIAL_SCHEDULE_BASE_URL", defaults.BaseURL),
		Transport: ProviderTransportConfig{
			Timeout: time.Duration(sharedenv.Int("OFFICIAL_SCHEDULE_TIMEOUT_SECONDS", int(defaults.Timeout/time.Second))) * time.Second,
		},
	}
}

func (c *YouTubeCollectorRuntimeConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("youtube collector runtime config is nil")
	}
	if err := validateUnsupportedLegacyEnvUsage(); err != nil {
		return err
	}
	if err := c.validateServer(); err != nil {
		return err
	}
	if err := c.validateTracing(); err != nil {
		return err
	}
	if err := c.validateOwnership(); err != nil {
		return err
	}
	if err := c.validatePostgres(); err != nil {
		return err
	}
	if err := c.Proxy.Validate(); err != nil {
		return err
	}
	if err := c.validateProviders(); err != nil {
		return err
	}
	return c.applyCollector()
}

func (c *YouTubeCollectorRuntimeConfig) validateServer() error {
	if c.Server.Port <= 0 {
		return fmt.Errorf("SERVER_PORT is required")
	}
	if err := validateServerTransports(&c.Server); err != nil {
		return err
	}
	return validateAPISecretKey(c.Environment, c.Server.APIKey)
}

func (c *YouTubeCollectorRuntimeConfig) validateTracing() error {
	if err := validateTracingConfig(c.Tracing); err != nil {
		return err
	}
	if !isProductionEnvironment(c.Environment) || c.Tracing.Enabled {
		return nil
	}
	enabledEnv, err := youtubeCollectorTracingEnabledEnv(c.Collector.InstanceID)
	if err != nil {
		return err
	}
	return fmt.Errorf("%s=true is required in production", enabledEnv)
}

func (c *YouTubeCollectorRuntimeConfig) validateOwnership() error {
	if err := c.validateCollectorOwnershipFlags(); err != nil {
		return err
	}
	if err := c.validateCollectorEgressOwnership(); err != nil {
		return err
	}
	return c.validateProductionCollectorOwnership()
}

func (c *YouTubeCollectorRuntimeConfig) validateCollectorOwnershipFlags() error {
	if !c.RuntimeOwnership.RuntimeAllowed {
		return fmt.Errorf("youtube collector runtime disabled: set YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true on the owning host")
	}
	if c.WorkerProfile == nil {
		return fmt.Errorf("youtube collector worker profile is required")
	}
	if c.RuntimeOwnership.PhotoSyncEnabled {
		return fmt.Errorf("%s requires PHOTO_SYNC_ENABLED=false", runtimeYouTubeCollector)
	}
	return nil
}

func (c *YouTubeCollectorRuntimeConfig) validateCollectorEgressOwnership() error {
	if err := validateNotificationRoleEnvValues(); err != nil {
		return err
	}
	if err := rejectReservedEgressRoles(runtimeYouTubeCollector); err != nil {
		return err
	}
	return nil
}

func (c *YouTubeCollectorRuntimeConfig) validateProductionCollectorOwnership() error {
	if !isProductionEnvironment(c.Environment) {
		return nil
	}
	if !strings.EqualFold(c.RuntimeOwnership.NotificationEgressRole, notificationEgressRoleOff) {
		return fmt.Errorf("%s production requires %s=%s", runtimeYouTubeCollector, notificationEgressRoleEnv, notificationEgressRoleOff)
	}
	return nil
}

func (c *YouTubeCollectorRuntimeConfig) validatePostgres() error {
	if err := validateYouTubeCollectorPostgresUser(c.Postgres.User); err != nil {
		return err
	}
	if err := validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	if !isProductionEnvironment(c.Environment) {
		return nil
	}
	if strings.TrimSpace(c.Postgres.Password) == "" {
		return fmt.Errorf("%s production requires POSTGRES_PASSWORD", runtimeYouTubeCollector)
	}
	return validateReadablePostgresSSLRootCert(c.Postgres.SSLRootCert)
}

func (c *YouTubeCollectorRuntimeConfig) validateProviders() error {
	if err := validateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return err
	}
	if err := validateHolodexTimeout(c.Holodex.Transport.Timeout); err != nil {
		return err
	}
	if err := validateOfficialScheduleBaseURL(c.OfficialSchedule.BaseURL); err != nil {
		return err
	}
	return validateOfficialScheduleTimeout(c.OfficialSchedule.Transport.Timeout)
}

func (c *YouTubeCollectorRuntimeConfig) applyCollector() error {
	if err := c.Collector.Validate(c.Holodex.Transport.Timeout, c.OfficialSchedule.Transport.Timeout); err != nil {
		return err
	}
	return nil
}

func applyYouTubeCollectorWorkerProfile(config *YouTubeCollectorRuntimeConfig) {
	profile := config.WorkerProfile
	worker := profile.Loaded.Profile.Workers["collection"]
	settings := profile.Collection
	config.Collector.TotalWorkers = worker.Executor.ConfiguredWorkers
	config.Collector.QueueCapacity = int(*worker.Queue.Capacity.Items)
	config.Collector.AcquisitionBatch = settings.AcquisitionBatch
	config.Collector.AcquisitionCadence = time.Duration(settings.AcquisitionCadenceMS) * time.Millisecond
	config.Collector.LeaseTTL = time.Duration(settings.LeaseTTLMS) * time.Millisecond
	config.Collector.RenewInterval = time.Duration(settings.RenewIntervalMS) * time.Millisecond
	config.Collector.RenewTimeout = time.Duration(settings.RenewTimeoutMS) * time.Millisecond
	config.Collector.DBTimeout = time.Duration(settings.DBTimeoutMS) * time.Millisecond
	config.Collector.CleanupTimeout = time.Duration(settings.CleanupTimeoutMS) * time.Millisecond
	config.Collector.ProviderAdmissionTimeout = time.Duration(settings.ProviderAdmissionTimeoutMS) * time.Millisecond
	config.Collector.CollectionOverhead = time.Duration(settings.CollectionOverheadMS) * time.Millisecond
	config.Collector.PublishTimeout = time.Duration(settings.PublishTimeoutMS) * time.Millisecond
	config.Collector.RetryMin = time.Duration(settings.RetryMinMS) * time.Millisecond
	config.Collector.RetryMax = time.Duration(settings.RetryMaxMS) * time.Millisecond
	config.Collector.ReleaseJitterMin = time.Duration(settings.ReleaseJitterMinMS) * time.Millisecond
	config.Collector.ReleaseJitterMax = time.Duration(settings.ReleaseJitterMaxMS) * time.Millisecond
	config.Collector.HolodexMaxInflight = settings.HolodexMaxInflight
	config.Collector.OfficialMaxInflight = settings.OfficialMaxInflight
	config.Collector.YouTubeJSMaxInflight = settings.YouTubeJSMaxInflight
}

func (c CollectorProxyConfig) Validate() error {
	if !c.Enabled {
		if c.URL == "" {
			return nil
		}
		return fmt.Errorf("SCRAPER_PROXY_URL must be empty when SCRAPER_PROXY_ENABLED=false (got %s)", redactedProxyURL(c.URL))
	}
	if c.URL == "" {
		return fmt.Errorf("SCRAPER_PROXY_URL is required when SCRAPER_PROXY_ENABLED=true")
	}
	return validateCollectorProxyURL(c.URL)
}

func validateCollectorProxyURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return fmt.Errorf("SCRAPER_PROXY_URL is invalid")
	}
	if err := validateCollectorProxyEndpoint(parsed, raw); err != nil {
		return err
	}
	return validateCollectorProxyResource(parsed, raw)
}

func validateCollectorProxyEndpoint(parsed *url.URL, raw string) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
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
		return fmt.Errorf("POSTGRES_SSLROOTCERT is required in production")
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

func resolvedHololiveScraperUser() string {
	user := strings.TrimSpace(sharedenv.String("HOLOLIVE_SCRAPER_USER", postgresScraperRoleUser))
	if user == "" {
		return postgresScraperRoleUser
	}
	return user
}
