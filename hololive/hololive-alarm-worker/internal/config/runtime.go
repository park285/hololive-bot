package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type RuntimeConfig struct {
	Environment          string
	Server               settings.ServerConfig
	Iris                 settings.IrisConfig
	Holodex              settings.HolodexConfig
	Valkey               settings.ValkeyConfig
	Postgres             settings.PostgresConfig
	Notification         settings.NotificationConfig
	Logging              settings.LoggingConfig
	SettingsFilePath     string
	MarkdownReplies      bool
	AlarmWorkerProfile   *AlarmWorkerProfile
	DispatchRetention    DispatchRetentionConfig
	AlarmDispatchPublish AlarmDispatchPublishConfig
	NotificationRoles    NotificationRoles
	Celebration          CelebrationRunnerConfig
	XSpaces              XSpacesConfig
}

type AlarmDispatchPublishConfig struct {
	MaxDeliveriesPerBatch int
}

func LoadRuntime() (*RuntimeConfig, error) {
	if err := envload.DotEnv(); err != nil {
		return nil, fmt.Errorf("load dot env: %w", err)
	}

	config, err := loadRuntimeConfig()
	if err != nil {
		return nil, err
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return config, nil
}

func loadRuntimeConfig() (*RuntimeConfig, error) {
	iris, err := settings.LoadIrisConfig()
	if err != nil {
		return nil, fmt.Errorf("load iris config: %w", err)
	}

	config, err := loadRuntimeSections()
	if err != nil {
		return nil, err
	}

	config.Iris = iris

	if err := validateEgressInputs(&config.Iris, config.Holodex.APIKey); err != nil {
		return nil, fmt.Errorf("validate required: %w", err)
	}

	if err := loadRoleSections(config); err != nil {
		return nil, err
	}

	return config, nil
}

func loadRuntimeSections() (*RuntimeConfig, error) {
	server, serverErr := settings.LoadServerConfig()
	holodex, holodexErr := settings.LoadHolodexConfig()
	valkey, valkeyErr := settings.LoadValkeyConfig()
	postgres, postgresErr := settings.LoadPostgresConfig()
	notification, notificationErr := settings.LoadNotificationConfig()
	logging, loggingErr := settings.LoadLoggingConfig()
	bot, botErr := settings.LoadBotConfig()

	if err := errors.Join(
		serverErr, holodexErr, valkeyErr, postgresErr, notificationErr, loggingErr, botErr,
	); err != nil {
		return nil, fmt.Errorf("load alarm-worker sections: %w", err)
	}

	return &RuntimeConfig{
		Environment:      envload.AppEnvironment(),
		Server:           server,
		Holodex:          holodex,
		Valkey:           valkey,
		Postgres:         postgres,
		Notification:     notification,
		Logging:          logging,
		SettingsFilePath: settings.LoadSettingsFilePath(),
		MarkdownReplies:  bot.MarkdownReplies,
	}, nil
}

func loadRoleSections(config *RuntimeConfig) error {
	profile, err := LoadWorkerProfile()
	if err != nil {
		return fmt.Errorf("load alarm worker profile: %w", err)
	}

	retention, err := loadDispatchRetentionConfig()
	if err != nil {
		return fmt.Errorf("load alarm dispatch retention config: %w", err)
	}

	maxDeliveries, err := envload.RequiredPositiveIntEnv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", 1000)
	if err != nil {
		return fmt.Errorf("load alarm dispatch publish config: %w", err)
	}

	celebration, err := loadCelebrationRunnerConfig()
	if err != nil {
		return fmt.Errorf("load celebration runner config: %w", err)
	}

	config.AlarmWorkerProfile = profile
	config.DispatchRetention = retention
	config.AlarmDispatchPublish = AlarmDispatchPublishConfig{MaxDeliveriesPerBatch: maxDeliveries}
	config.NotificationRoles = loadNotificationRoles()
	config.Celebration = celebration
	config.XSpaces = loadXSpacesConfig()

	return nil
}

func validateEgressInputs(iris *settings.IrisConfig, holodexAPIKey string) error {
	if err := settings.ValidateIrisClientInputs(iris); err != nil {
		return fmt.Errorf("validate iris client inputs: %w", err)
	}

	if err := runtimepolicy.ValidateHolodexAPIKey(holodexAPIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	return nil
}

func (c *RuntimeConfig) validate() error {
	if c.Postgres.PoolMaxConns < 2 {
		return errors.New("POSTGRES_POOL_MAX_CONNS must be at least 2 to reserve alarm control capacity")
	}

	if c.Postgres.PoolMinConns < 0 || c.Postgres.PoolMinConns > c.Postgres.PoolMaxConns {
		return errors.New("POSTGRES_POOL_MIN_CONNS must be nonnegative and not exceed POSTGRES_POOL_MAX_CONNS")
	}

	if strings.TrimSpace(c.Postgres.User) != runtimepolicy.PostgresAlarmWorkerRoleUser {
		return fmt.Errorf("alarm-worker requires POSTGRES_USER=%s", runtimepolicy.PostgresAlarmWorkerRoleUser)
	}

	if err := settings.ValidateServerRuntime(c.Environment, &c.Server); err != nil {
		return fmt.Errorf("validate server runtime: %w", err)
	}

	if err := runtimepolicy.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := settings.ValidateHolodexConfig(&c.Holodex); err != nil {
		return fmt.Errorf("validate holodex config: %w", err)
	}

	if c.AlarmWorkerProfile == nil {
		return errors.New("alarm-worker runtime requires Stack Worker Profile v1")
	}

	if err := validateOwnership(c.Environment, c.NotificationRoles); err != nil {
		return fmt.Errorf("validate alarm worker ownership: %w", err)
	}

	if err := c.XSpaces.validate(); err != nil {
		return fmt.Errorf("validate X spaces config: %w", err)
	}

	if !runtimepolicy.IsProduction(c.Environment) {
		return nil
	}

	if err := validateProductionExecutors(c.AlarmWorkerProfile); err != nil {
		return fmt.Errorf("validate production alarm executors: %w", err)
	}

	return nil
}
