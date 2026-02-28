package config

import (
	"fmt"

	"github.com/joho/godotenv"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// AdminAPIConfig: admin-api 바이너리 전용 설정
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

// LoadAdminAPI: admin-api 전용 설정을 환경변수에서 로드합니다.
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

	return &AdminAPIConfig{
		Server: ServerConfig{
			Port:   envutil.Int("ADMIN_API_PORT", 30002),
			APIKey: envutil.String("API_SECRET_KEY", ""),
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Holodex: HolodexConfig{
			BaseURL: envutil.String("HOLODEX_BASE_URL", constants.APIConfig.HolodexBaseURL),
			APIKeys: collectAPIKeys("HOLODEX_API_KEY_"),
		},
		CORS: CORSConfig{
			AllowedOrigins:      corsAllowedOrigins,
			Enforce:             envutil.Bool("CORS_ENFORCE", false),
			MissingInProduction: corsMissingInProduction,
		},
		Telemetry: loadTelemetryConfig(),
		Services: ServicesConfig{
			LLMServerHealthURL:      envutil.String("SERVICES_LLM_SERVER_HEALTH_URL", ""),
			GameBotTwentyQHealthURL: envutil.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
			GameBotTurtleHealthURL:  envutil.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
		},
		Logging: LoggingConfig{
			Level:      envutil.String("LOG_LEVEL", "info"),
			Dir:        envutil.String("LOG_DIR", ""),
			MaxSizeMB:  envutil.Int("LOG_MAX_SIZE_MB", 100),
			MaxBackups: envutil.Int("LOG_MAX_BACKUPS", 5),
			MaxAgeDays: envutil.Int("LOG_MAX_AGE_DAYS", 30),
			Compress:   envutil.Bool("LOG_COMPRESS", true),
		},
		AlarmDispatcherURL: envutil.String("ALARM_DISPATCHER_URL", ""),
		LLMSchedulerURL:    envutil.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		Version:            envutil.String("APP_VERSION", "1.0.0-admin-api"),
	}
}

// validate: 필수 설정값을 검증합니다.
func (c *AdminAPIConfig) validate() error {
	if len(c.Holodex.APIKeys) == 0 {
		return fmt.Errorf("at least one HOLODEX_API_KEY is required")
	}
	return nil
}
