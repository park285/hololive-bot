package config

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type adminAPIEnvConfig struct {
	AdminAPIPort int    `envconfig:"ADMIN_API_PORT" default:"30002"`
	APISecretKey string `envconfig:"API_SECRET_KEY"`

	HolodexBaseURL string `envconfig:"HOLODEX_BASE_URL"`

	ServicesLLMServerHealthURL      string `envconfig:"SERVICES_LLM_SERVER_HEALTH_URL"`
	ServicesGameBotTwentyQHealthURL string `envconfig:"SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL"`
	ServicesGameBotTurtleHealthURL  string `envconfig:"SERVICES_GAME_BOT_TURTLE_HEALTH_URL"`

	LogLevel           string `envconfig:"LOG_LEVEL" default:"info"`
	AlarmDispatcherURL string `envconfig:"ALARM_DISPATCHER_URL"`
	LLMSchedulerURL    string `envconfig:"LLM_SCHEDULER_INTERNAL_URL"`
	AppVersion         string `envconfig:"APP_VERSION" default:"1.0.0-bot-admin"`
	CORSEnforce        string `envconfig:"CORS_ENFORCE" default:"false"`
}

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

	cfg, err := buildAdminAPIConfig()
	if err != nil {
		return nil, fmt.Errorf("build admin api config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("admin api config validation failed: %w", err)
	}

	return cfg, nil
}

func buildAdminAPIConfig() (*AdminAPIConfig, error) {
	_, _, corsAllowedOrigins, corsMissingInProduction := loadRuntimeTokensAndCORS()

	var raw adminAPIEnvConfig
	if err := envconfig.Process("", &raw); err != nil {
		return nil, fmt.Errorf("process env: %w", err)
	}

	holodexBaseURL := strings.TrimSpace(raw.HolodexBaseURL)
	if holodexBaseURL == "" {
		holodexBaseURL = constants.APIConfig.HolodexBaseURL
	}
	logLevel := strings.TrimSpace(raw.LogLevel)
	if logLevel == "" {
		logLevel = "info"
	}
	version := strings.TrimSpace(raw.AppVersion)
	if version == "" {
		version = "1.0.0-bot-admin"
	}

	return &AdminAPIConfig{
		Server: ServerConfig{
			Port:   raw.AdminAPIPort,
			APIKey: strings.TrimSpace(raw.APISecretKey),
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Holodex: HolodexConfig{
			BaseURL: holodexBaseURL,
			APIKeys: collectAPIKeys("HOLODEX_API_KEY_"),
		},
		CORS: CORSConfig{
			AllowedOrigins:      corsAllowedOrigins,
			Enforce:             parseBoolWithDefault(raw.CORSEnforce, false),
			MissingInProduction: corsMissingInProduction,
		},
		Telemetry: loadTelemetryConfig(),
		Services: ServicesConfig{
			LLMServerHealthURL:      strings.TrimSpace(raw.ServicesLLMServerHealthURL),
			GameBotTwentyQHealthURL: strings.TrimSpace(raw.ServicesGameBotTwentyQHealthURL),
			GameBotTurtleHealthURL:  strings.TrimSpace(raw.ServicesGameBotTurtleHealthURL),
		},
		Logging: LoggingConfig{
			Level: logLevel,
		},
		AlarmDispatcherURL: strings.TrimSpace(raw.AlarmDispatcherURL),
		LLMSchedulerURL:    strings.TrimSpace(raw.LLMSchedulerURL),
		Version:            version,
	}, nil
}

// validate: 필수 설정값을 검증합니다.
func (c *AdminAPIConfig) validate() error {
	if err := validateAPISecretKey(c.Telemetry.Environment, c.Server.APIKey); err != nil {
		return err
	}
	if len(c.Holodex.APIKeys) == 0 {
		return fmt.Errorf("at least one HOLODEX_API_KEY is required")
	}
	if err := validatePostgresSSLMode(c.Telemetry.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	return nil
}
