package config

import (
	"errors"
	"fmt"

	sharedenv "github.com/park285/shared-go/core/envutil"
	sharedh3 "github.com/park285/shared-go/transport/h3"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type AdminPlaneConfig struct {
	Environment                 string
	Server                      settings.ServerConfig
	InternalH3                  sharedh3.ClientOptions
	Postgres                    settings.PostgresConfig
	Notification                settings.NotificationConfig
	Ingestion                   IngestionConfig
	SettingsFilePath            string
	AlarmServiceURL             string
	XSpacesKeyFile              string
	SchedulePosterConfigFile    string
	SchedulePosterGeminiKeyFile string
}

func loadAdminPlaneConfig() (*AdminPlaneConfig, error) {
	server, serverErr := settings.LoadServerConfig()
	postgres, postgresErr := settings.LoadPostgresEndpointConfig()
	notification, notificationErr := settings.LoadNotificationConfig()
	ingestion, ingestionErr := loadIngestionConfig()

	if err := errors.Join(
		serverErr, postgresErr, notificationErr, ingestionErr,
	); err != nil {
		return nil, fmt.Errorf("load admin plane sections: %w", err)
	}

	config := &AdminPlaneConfig{
		Environment:                 envload.AppEnvironment(),
		Server:                      server,
		InternalH3:                  loadInternalH3ClientOptions(),
		Postgres:                    postgres,
		Notification:                notification,
		Ingestion:                   ingestion,
		SettingsFilePath:            settings.LoadSettingsFilePath(),
		XSpacesKeyFile:              sharedenv.StringRaw("X_SPACES_KEY_FILE", ""),
		SchedulePosterConfigFile:    sharedenv.StringRaw("SCHEDULE_POSTER_CONFIG_FILE", ""),
		SchedulePosterGeminiKeyFile: sharedenv.StringRaw("SCHEDULE_POSTER_GEMINI_API_KEY_FILE", ""),
	}

	return config, nil
}

type IngestionConfig struct {
	PhotoSyncEnabled bool
}

func loadIngestionConfig() (IngestionConfig, error) {
	photoSyncEnabled, err := sharedenv.BoolE("PHOTO_SYNC_ENABLED", true)
	if err != nil {
		return IngestionConfig{}, fmt.Errorf("load ingestion config: %w", err)
	}

	return IngestionConfig{PhotoSyncEnabled: photoSyncEnabled}, nil
}

func (c *AdminPlaneConfig) Validate() error {
	if c == nil {
		return errors.New("admin plane config is required")
	}

	if err := settings.ValidateServerRuntime(c.Environment, &c.Server); err != nil {
		return fmt.Errorf("validate server runtime: %w", err)
	}

	if err := runtimepolicy.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeAdminAPI, sharedenv.String(runtimepolicy.NotificationEgressRoleEnv, ""), sharedenv.String(runtimepolicy.NotificationSchedulerRoleEnv, "")); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}
