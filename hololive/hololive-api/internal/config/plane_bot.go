package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/core/envutil"
	sharedh3 "github.com/park285/shared-go/transport/h3"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type BotPlaneConfig struct {
	Environment                string
	Server                     settings.ServerConfig
	InternalH3                 sharedh3.ClientOptions
	Iris                       settings.IrisConfig
	Kakao                      KakaoConfig
	Holodex                    settings.HolodexConfig
	OfficialSchedule           settings.OfficialScheduleRuntimeConfig
	Valkey                     settings.ValkeyConfig
	Postgres                   settings.PostgresConfig
	Bot                        settings.BotConfig
	Webhook                    WebhookConfig
	APIWorkerProfile           *APIWorkerProfile
	AlarmServiceURL            string
	BroadcastTypeGeminiKeyFile string
}

type WebhookConfig struct {
	MaxBodyBytes int64
	DedupTimeout time.Duration
}

func LoadBotPlaneRuntime() (*BotPlaneConfig, error) {
	if err := loadProcessEnv(); err != nil {
		return nil, err
	}

	config, err := loadBotPlaneConfig()
	if err != nil {
		return nil, fmt.Errorf("load bot plane config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("bot plane config validation failed: %w", err)
	}

	return config, nil
}

func loadProcessEnv() error {
	if err := envload.DotEnv(); err != nil {
		return fmt.Errorf("load dot env: %w", err)
	}

	return nil
}

func loadInternalH3ClientOptions() sharedh3.ClientOptions {
	return sharedh3.ClientOptions{
		CACertFile: strings.TrimSpace(os.Getenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE")),
		ServerName: strings.TrimSpace(os.Getenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME")),
	}
}

func loadBotPlaneConfig() (*BotPlaneConfig, error) {
	iris, err := settings.LoadIrisConfig()
	if err != nil {
		return nil, fmt.Errorf("load iris config: %w", err)
	}

	kakao, err := loadKakaoConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kakao config: %w", err)
	}

	server, serverErr := settings.LoadServerConfig()
	holodex, holodexErr := settings.LoadHolodexConfig()
	officialSchedule, officialScheduleErr := settings.LoadOfficialScheduleRuntimeConfig()
	valkey, valkeyErr := settings.LoadValkeyConfig()
	postgres, postgresErr := settings.LoadPostgresEndpointConfig()
	bot, botErr := settings.LoadBotConfig()

	if err = errors.Join(serverErr, holodexErr, officialScheduleErr, valkeyErr, postgresErr, botErr); err != nil {
		return nil, fmt.Errorf("load bot plane sections: %w", err)
	}

	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		return nil, fmt.Errorf("load API worker profile: %w", err)
	}

	config := &BotPlaneConfig{
		Environment:      envload.AppEnvironment(),
		Server:           server,
		InternalH3:       loadInternalH3ClientOptions(),
		Iris:             iris,
		Kakao:            kakao,
		Holodex:          holodex,
		OfficialSchedule: officialSchedule,
		Valkey:           valkey,
		Postgres:         postgres,
		Bot:              bot,
		Webhook:          webhookConfigFromProfile(profile),
		APIWorkerProfile: profile,
		AlarmServiceURL:  sharedenv.String("ALARM_INTERNAL_URL", ""),

		BroadcastTypeGeminiKeyFile: sharedenv.StringRaw("BROADCAST_TYPE_GEMINI_API_KEY_FILE", ""),
	}

	return config, nil
}

func webhookConfigFromProfile(profile *APIWorkerProfile) WebhookConfig {
	return WebhookConfig{
		MaxBodyBytes: profile.BotWebhookInbox.MaxBodyBytes,
		DedupTimeout: workercontract.Milliseconds(profile.BotWebhookInbox.DedupTimeoutMS),
	}
}

func (c *BotPlaneConfig) Validate() error {
	if c == nil {
		return errors.New("bot plane config is required")
	}

	if err := settings.ValidateServerRuntime(c.Environment, &c.Server); err != nil {
		return fmt.Errorf("validate server runtime: %w", err)
	}

	if err := c.validateRequired(); err != nil {
		return fmt.Errorf("validate required: %w", err)
	}

	if err := validatePlaneDependencies(c.Environment, &c.Postgres, &c.Holodex, c.OfficialSchedule); err != nil {
		return err
	}

	if c.APIWorkerProfile == nil {
		return errors.New("bot runtime requires Stack Worker Profile v1")
	}

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeBot, sharedenv.String(runtimepolicy.NotificationEgressRoleEnv, ""), sharedenv.String(runtimepolicy.NotificationSchedulerRoleEnv, "")); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}

func (c *BotPlaneConfig) validateRequired() error {
	if err := validateKakaoRooms(c.Kakao.Rooms); err != nil {
		return fmt.Errorf("validate kakao rooms: %w", err)
	}

	if err := settings.ValidateIrisEgressInputs(&c.Iris); err != nil {
		return fmt.Errorf("validate iris egress inputs: %w", err)
	}

	if err := runtimepolicy.ValidateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	return nil
}

func validatePlaneDependencies(
	environment string,
	postgres *settings.PostgresConfig,
	holodex *settings.HolodexConfig,
	officialSchedule settings.OfficialScheduleRuntimeConfig,
) error {
	if err := runtimepolicy.ValidatePostgresSSLMode(environment, postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := settings.ValidateHolodexConfig(holodex); err != nil {
		return fmt.Errorf("validate holodex config: %w", err)
	}

	if err := settings.ValidateOfficialScheduleRuntimeConfig(officialSchedule); err != nil {
		return fmt.Errorf("validate official schedule config: %w", err)
	}

	return nil
}
