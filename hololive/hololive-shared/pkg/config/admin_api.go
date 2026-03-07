package config

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"

	"github.com/kapu/hololive-shared/internal/envutil"
	"github.com/kapu/hololive-shared/pkg/constants"
)

// AdminAPIConfig: 운영 API(호환) 설정
type AdminAPIConfig struct {
	Server             ServerConfig
	Valkey             ValkeyConfig
	Postgres           PostgresConfig
	Holodex            HolodexConfig
	CORS               CORSConfig
	Telemetry          TelemetryConfig
	Services           ServicesConfig
	Logging            LoggingConfig
	AlarmDispatcherURL string // alarm-dispatcher HTTP 기반 CRUD URL
	LLMSchedulerURL    string // llm-scheduler 내부 트리거 프록시 URL
	Version            string
}

// LoadAdminAPI: 운영 API(호환) 설정을 환경변수에서 로드합니다.
func LoadAdminAPI() (*AdminAPIConfig, error) {
	_ = godotenv.Load()

	cfg := buildAdminAPIConfig()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("admin api config validation failed: %w", err)
	}

	return cfg, nil
}

func buildAdminAPIConfig() *AdminAPIConfig {
	_, _, corsAllowedOrigins, corsMissingInProduction := loadRuntimeTokensAndCORS()
	llmSchedulerHealthURL := envutil.StringAny(
		"SERVICES_LLM_SCHEDULER_HEALTH_URL",
		"SERVICES_LLM_SERVER_HEALTH_URL",
	)

	return &AdminAPIConfig{
		Server: ServerConfig{
			Port:   envutil.Int("ADMIN_API_PORT", 30002),
			APIKey: envutil.String("API_SECRET_KEY", ""),
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Holodex: HolodexConfig{
			BaseURL: envutil.String("HOLODEX_BASE_URL", constants.APIConfig.HolodexBaseURL),
			APIKey:  resolveHolodexAPIKey(),
		},
		CORS: CORSConfig{
			AllowedOrigins:      corsAllowedOrigins,
			Enforce:             envutil.Bool("CORS_ENFORCE", false),
			MissingInProduction: corsMissingInProduction,
		},
		Telemetry: loadTelemetryConfig(),
		Services: ServicesConfig{
			LLMSchedulerHealthURL:   llmSchedulerHealthURL,
			GameBotTwentyQHealthURL: envutil.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
			GameBotTurtleHealthURL:  envutil.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
		},
		Logging: LoggingConfig{
			Level: envutil.String("LOG_LEVEL", "info"),
		},
		AlarmDispatcherURL: envutil.String("ALARM_DISPATCHER_URL", ""),
		LLMSchedulerURL:    envutil.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		Version:            envutil.String("APP_VERSION", "1.0.0-bot-admin"),
	}
}

// validate: 필수 설정값을 검증합니다.
func (c *AdminAPIConfig) validate() error {
	if err := validateAPISecretKey(c.Telemetry.Environment, c.Server.APIKey); err != nil {
		return err
	}
	if strings.TrimSpace(c.Holodex.APIKey) == "" {
		return fmt.Errorf("HOLODEX_API_KEY is required")
	}
	if err := validatePostgresSSLMode(c.Telemetry.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	return nil
}
