package settings

import (
	"errors"
	"fmt"
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
		return nil, fmt.Errorf("load youtube collector worker profile: %w", err)
	}

	collector, err := loadYouTubeCollectorConfig()
	if err != nil {
		return nil, fmt.Errorf("load youtube collector config: %w", err)
	}

	tracingConfig, err := loadTracingConfig(tracingRuntimeYouTubeCollector, collector.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("load tracing config: %w", err)
	}

	ownership, err := loadCollectorRuntimeOwnershipConfig()
	if err != nil {
		return nil, fmt.Errorf("load collector runtime ownership config: %w", err)
	}

	proxy, err := loadCollectorProxyConfig()
	if err != nil {
		return nil, fmt.Errorf("load collector proxy config: %w", err)
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
		return CollectorRuntimeOwnershipConfig{}, fmt.Errorf("read bool env: %w", err)
	}

	photoSyncEnabled, err := sharedenv.BoolE("PHOTO_SYNC_ENABLED", false)
	if err != nil {
		return CollectorRuntimeOwnershipConfig{}, fmt.Errorf("read bool env: %w", err)
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
		return CollectorProxyConfig{}, fmt.Errorf("read bool env: %w", err)
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
		return errors.New("youtube collector runtime config is nil")
	}

	if err := validateUnsupportedLegacyEnvUsage(); err != nil {
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

func (c *YouTubeCollectorRuntimeConfig) validateRuntimeDependencies() error {
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

func (c *YouTubeCollectorRuntimeConfig) validateServer() error {
	if c.Server.Port <= 0 {
		return errors.New("SERVER_PORT is required")
	}

	if err := validateServerTransports(&c.Server); err != nil {
		return fmt.Errorf("validate server transports: %w", err)
	}

	if err := validateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return fmt.Errorf("validate API secret key: %w", err)
	}

	return nil
}

func (c *YouTubeCollectorRuntimeConfig) validateTracing() error {
	if err := validateTracingConfig(c.Tracing); err != nil {
		return fmt.Errorf("validate tracing config: %w", err)
	}

	if !isProductionEnvironment(c.Environment) || c.Tracing.Enabled {
		return nil
	}

	enabledEnv, err := youtubeCollectorTracingEnabledEnv(c.Collector.InstanceID)
	if err != nil {
		return fmt.Errorf("youtube collector tracing enabled env: %w", err)
	}

	return fmt.Errorf("%s=true is required in production", enabledEnv)
}

func (c *YouTubeCollectorRuntimeConfig) validateOwnership() error {
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

func (c *YouTubeCollectorRuntimeConfig) validateCollectorOwnershipFlags() error {
	if !c.RuntimeOwnership.RuntimeAllowed {
		return errors.New("youtube collector runtime disabled: set YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true on the owning host")
	}

	if c.WorkerProfile == nil {
		return errors.New("youtube collector worker profile is required")
	}

	if c.RuntimeOwnership.PhotoSyncEnabled {
		return fmt.Errorf("%s requires PHOTO_SYNC_ENABLED=false", runtimeYouTubeCollector)
	}

	return nil
}

func (c *YouTubeCollectorRuntimeConfig) validateCollectorEgressOwnership() error {
	if err := validateNotificationRoleEnvValues(); err != nil {
		return fmt.Errorf("validate notification role env values: %w", err)
	}

	if err := rejectReservedEgressRoles(runtimeYouTubeCollector); err != nil {
		return fmt.Errorf("reject reserved egress roles: %w", err)
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
		return fmt.Errorf("validate youtube collector postgres user: %w", err)
	}

	if err := validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if !isProductionEnvironment(c.Environment) {
		return nil
	}

	if strings.TrimSpace(c.Postgres.Password) == "" {
		return fmt.Errorf("%s production requires POSTGRES_PASSWORD", runtimeYouTubeCollector)
	}

	if err := validateReadablePostgresSSLRootCert(c.Postgres.SSLRootCert); err != nil {
		return fmt.Errorf("validate readable postgres SSL root cert: %w", err)
	}

	return nil
}
