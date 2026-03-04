package config

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

type llmSchedulerEnvConfig struct {
	LLMSchedulerPort int    `envconfig:"LLM_SCHEDULER_PORT" default:"30003"`
	APISecretKey     string `envconfig:"API_SECRET_KEY"`
	IrisBaseURL      string `envconfig:"IRIS_BASE_URL" default:"http://localhost:3000"`

	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`

	BotPrefix   string `envconfig:"BOT_PREFIX" default:"!"`
	BotSelfUser string `envconfig:"BOT_SELF_USER" default:"iris"`

	AppVersion string `envconfig:"APP_VERSION" default:"1.0.0-llm-scheduler"`
}

// LLMSchedulerConfig: llm-scheduler 바이너리 전용 설정
type LLMSchedulerConfig struct {
	Server    ServerConfig
	Iris      IrisConfig
	Valkey    ValkeyConfig
	Postgres  PostgresConfig
	Logging   LoggingConfig
	Bot       BotConfig
	Telemetry TelemetryConfig
	Cliproxy  CliproxyConfig
	LLM       LLMConfig
	Exa       ExaConfig
	Version   string
}

// LoadLLMScheduler: llm-scheduler 전용 설정을 환경변수에서 로드합니다.
func LoadLLMScheduler() (*LLMSchedulerConfig, error) {
	_ = godotenv.Load()

	cfg, err := buildLLMSchedulerConfig()
	if err != nil {
		return nil, fmt.Errorf("build llm scheduler config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("llm scheduler config validation failed: %w", err)
	}
	return cfg, nil
}

func buildLLMSchedulerConfig() (*LLMSchedulerConfig, error) {
	webhookToken, botToken, _, _ := loadRuntimeTokensAndCORS()

	var raw llmSchedulerEnvConfig
	if err := envconfig.Process("", &raw); err != nil {
		return nil, fmt.Errorf("process env: %w", err)
	}
	logLevel := strings.TrimSpace(raw.LogLevel)
	if logLevel == "" {
		logLevel = "info"
	}
	baseURL := strings.TrimSpace(raw.IrisBaseURL)
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}
	botPrefix := strings.TrimSpace(raw.BotPrefix)
	if botPrefix == "" {
		botPrefix = "!"
	}
	botSelfUser := stringutil.TrimSpace(raw.BotSelfUser)
	if botSelfUser == "" {
		botSelfUser = "iris"
	}
	version := strings.TrimSpace(raw.AppVersion)
	if version == "" {
		version = "1.0.0-llm-scheduler"
	}

	return &LLMSchedulerConfig{
		Server: ServerConfig{
			Port:   raw.LLMSchedulerPort,
			APIKey: strings.TrimSpace(raw.APISecretKey),
		},
		Iris: IrisConfig{
			BaseURL:      baseURL,
			WebhookToken: webhookToken,
			BotToken:     botToken,
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Logging: LoggingConfig{
			Level: logLevel,
		},
		Bot: BotConfig{
			Prefix:   botPrefix,
			SelfUser: botSelfUser,
		},
		Telemetry: loadTelemetryConfig(),
		Cliproxy:  loadCliproxyConfig(),
		LLM:       loadLLMConfig(),
		Exa:       loadExaConfig(),
		Version:   version,
	}, nil
}

func (c *LLMSchedulerConfig) validate() error {
	if c.Server.Port == 0 {
		return fmt.Errorf("LLM_SCHEDULER_PORT is required")
	}
	if err := validateAPISecretKey(c.Telemetry.Environment, c.Server.APIKey); err != nil {
		return err
	}
	if strings.TrimSpace(c.Iris.WebhookToken) == "" {
		return fmt.Errorf("IRIS_WEBHOOK_TOKEN (or IRIS_SHARED_TOKEN) is required")
	}
	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return fmt.Errorf("IRIS_BOT_TOKEN (or IRIS_SHARED_TOKEN) is required")
	}
	if err := validatePostgresSSLMode(c.Telemetry.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	return nil
}
