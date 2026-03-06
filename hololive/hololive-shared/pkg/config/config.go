package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	sharedirisx "github.com/park285/llm-kakao-bots/shared-go/pkg/irisx"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/internal/envutil"
	"github.com/kapu/hololive-shared/pkg/constants"
)

const maxHolodexAPIKeySlots = 5

// Config: 홀로라이브 봇의 전체 동작에 필요한 설정을 담는 구조체
type Config struct {
	Iris               IrisConfig
	Server             ServerConfig
	Kakao              KakaoConfig
	Holodex            HolodexConfig
	YouTube            YouTubeConfig
	Chzzk              ChzzkConfig // 치지직 Open API 설정
	Twitch             TwitchConfig
	Valkey             ValkeyConfig
	Postgres           PostgresConfig
	Notification       NotificationConfig
	Logging            LoggingConfig
	Bot                BotConfig
	Services           ServicesConfig
	Telemetry          TelemetryConfig // OpenTelemetry 분산 추적
	Scraper            ScraperConfig   // YouTube 스크래퍼 프록시 설정
	Webhook            WebhookConfig
	CORS               CORSConfig // CORS 설정
	Cliproxy           CliproxyConfig
	LLM                LLMConfig
	Exa                ExaConfig
	AlarmDispatcherURL string // alarm-dispatcher HTTP 기반 CRUD 전환 URL
	LLMSchedulerURL    string // llm-scheduler 내부 API URL (bot이 구독/다이제스트 요청 시 사용)
	Version            string
}

// Load: .env 파일 및 환경 변수로부터 설정을 로드하고, 기본값을 적용하여 Config 객체를 생성합니다.
func Load() (*Config, error) {
	_ = godotenv.Load()

	webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction := loadRuntimeTokensAndCORS()
	cfg := buildConfig(webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func loadRuntimeTokensAndCORS() (string, string, []string, bool) {
	sharedIrisToken := envutil.String("IRIS_SHARED_TOKEN", "")
	webhookToken, botToken := sharedirisx.ResolveTokens(
		envutil.String("IRIS_WEBHOOK_TOKEN", ""),
		envutil.String("IRIS_BOT_TOKEN", ""),
		sharedIrisToken,
	)

	runtimeEnv := envutil.String("APP_ENV", envutil.String("OTEL_ENVIRONMENT", "production"))
	isProduction := strings.EqualFold(runtimeEnv, "production")
	corsAllowedOrigins, corsMissingInProduction := parseCORSAllowedOrigins(
		envutil.String("CORS_ALLOWED_ORIGINS", ""),
		isProduction,
	)

	return webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction
}

func buildConfig(webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) *Config {
	llmSchedulerHealthURL := envutil.StringAny(
		"SERVICES_LLM_SCHEDULER_HEALTH_URL",
		"SERVICES_LLM_SERVER_HEALTH_URL",
	)

	return &Config{
		Iris: IrisConfig{
			BaseURL:                   envutil.String("IRIS_BASE_URL", "http://localhost:3000"),
			WebhookToken:              webhookToken,
			BotToken:                  botToken,
			HTTPTimeout:               time.Duration(envutil.Int("IRIS_HTTP_TIMEOUT_SECONDS", 10)) * time.Second,
			HTTPDialTimeout:           time.Duration(envutil.Int("IRIS_HTTP_DIAL_TIMEOUT_SECONDS", 3)) * time.Second,
			HTTPResponseHeaderTimeout: time.Duration(envutil.Int("IRIS_HTTP_RESP_HEADER_TIMEOUT_SECONDS", 5)) * time.Second,
		},
		Server: ServerConfig{
			Port:   envutil.Int("SERVER_PORT", 30001),
			APIKey: envutil.String("API_SECRET_KEY", ""),
		},
		Kakao: KakaoConfig{
			Rooms:      parseCommaSeparated(envutil.String("KAKAO_ROOMS", "홀로라이브 알림방")),
			ACLEnabled: envutil.Bool("KAKAO_ACL_ENABLED", true),
		},
		Holodex: HolodexConfig{
			BaseURL: envutil.String("HOLODEX_BASE_URL", constants.APIConfig.HolodexBaseURL),
			APIKeys: collectAPIKeys("HOLODEX_API_KEY_"),
		},
		YouTube: YouTubeConfig{
			APIKey:              envutil.String("YOUTUBE_API_KEY", ""),
			EnableQuotaBuilding: envutil.Bool("YOUTUBE_ENABLE_QUOTA_BUILDING", false),
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Notification: NotificationConfig{
			AdvanceMinutes: parseIntList(envutil.String("NOTIFICATION_ADVANCE_MINUTES", "5")),
			CheckInterval:  time.Duration(envutil.Int("CHECK_INTERVAL_SECONDS", 60)) * time.Second,
		},
		Logging: LoggingConfig{
			Level:      envutil.String("LOG_LEVEL", "info"),
			Dir:        envutil.String("LOG_DIR", ""),
			MaxSizeMB:  envutil.Int("LOG_MAX_SIZE_MB", 100),
			MaxBackups: envutil.Int("LOG_MAX_BACKUPS", 5),
			MaxAgeDays: envutil.Int("LOG_MAX_AGE_DAYS", 30),
			Compress:   envutil.Bool("LOG_COMPRESS", true),
		},
		Bot: BotConfig{
			Prefix:           envutil.String("BOT_PREFIX", "!"),
			SelfUser:         envutil.String("BOT_SELF_USER", "iris"),
			IngestionEnabled: envutil.Bool("BOT_INGESTION_ENABLED", true),
			AdminEnabled:     envutil.Bool("BOT_ADMIN_ENABLED", true),
		},
		Services: ServicesConfig{
			LLMSchedulerHealthURL:   llmSchedulerHealthURL,
			GameBotTwentyQHealthURL: envutil.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
			GameBotTurtleHealthURL:  envutil.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
		},
		Telemetry: loadTelemetryConfig(),
		Scraper: ScraperConfig{
			ProxyEnabled: envutil.Bool("SCRAPER_PROXY_ENABLED", false),
			ProxyURL:     envutil.String("SCRAPER_PROXY_URL", ""),
		},
		Webhook: WebhookConfig{
			WorkerCount:    envutil.Int("WEBHOOK_WORKER_COUNT", 16),
			QueueSize:      envutil.Int("WEBHOOK_QUEUE_SIZE", 1000),
			EnqueueTimeout: time.Duration(envutil.Int("WEBHOOK_ENQUEUE_TIMEOUT_MS", 50)) * time.Millisecond,
			HandlerTimeout: time.Duration(envutil.Int("WEBHOOK_HANDLER_TIMEOUT_SECONDS", 30)) * time.Second,
			RequireHTTP2:   envutil.Bool("WEBHOOK_REQUIRE_HTTP2", false),
		},
		Chzzk: ChzzkConfig{
			ClientID:     envutil.String("CHZZK_CLIENT_ID", ""),
			ClientSecret: envutil.String("CHZZK_CLIENT_SECRET", ""),
		},
		Twitch: TwitchConfig{
			ClientID:     envutil.String("TWITCH_CLIENT_ID", ""),
			ClientSecret: envutil.String("TWITCH_CLIENT_SECRET", ""),
		},
		Cliproxy:           loadCliproxyConfig(),
		LLM:                loadLLMConfig(),
		Exa:                loadExaConfig(),
		AlarmDispatcherURL: envutil.String("ALARM_DISPATCHER_URL", ""),
		LLMSchedulerURL:    envutil.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		CORS: CORSConfig{
			AllowedOrigins:      corsAllowedOrigins,
			Enforce:             envutil.Bool("CORS_ENFORCE", false),
			MissingInProduction: corsMissingInProduction,
		},
		Version: envutil.String("APP_VERSION", "1.1.0-go"),
	}
}

// Validate: 필수 설정값이 누락되지 않았는지 검증합니다.
func (c *Config) Validate() error {
	if err := validateDeprecatedEnvUsage(); err != nil {
		return err
	}
	if c.Server.Port == 0 {
		return fmt.Errorf("SERVER_PORT is required")
	}
	if err := validateAPISecretKey(c.Telemetry.Environment, c.Server.APIKey); err != nil {
		return err
	}
	if len(c.Kakao.Rooms) == 0 {
		return fmt.Errorf("KAKAO_ROOMS is required")
	}
	if strings.TrimSpace(c.Iris.WebhookToken) == "" {
		return fmt.Errorf("IRIS_WEBHOOK_TOKEN (or IRIS_SHARED_TOKEN) is required")
	}
	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return fmt.Errorf("IRIS_BOT_TOKEN (or IRIS_SHARED_TOKEN) is required")
	}
	if len(c.Holodex.APIKeys) == 0 {
		return fmt.Errorf("at least one HOLODEX_API_KEY is required")
	}
	if err := validatePostgresSSLMode(c.Telemetry.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	isProduction := strings.EqualFold(strings.TrimSpace(c.Telemetry.Environment), "production")
	if isProduction && c.CORS.Enforce && len(c.CORS.AllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true")
	}
	return nil
}

func validateDeprecatedEnvUsage() error {
	if value, exists := os.LookupEnv("MEMBER_NEWS_CLIPROXY_MODEL"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("MEMBER_NEWS_CLIPROXY_MODEL is no longer supported; use MEMBER_NEWS_LLM_MODEL")
	}
	if value, exists := os.LookupEnv("DB_SSLMODE"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("DB_SSLMODE is no longer supported; use POSTGRES_SSLMODE")
	}
	if value, exists := os.LookupEnv("DB_QUERY_EXEC_MODE"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("DB_QUERY_EXEC_MODE is no longer supported; use POSTGRES_QUERY_EXEC_MODE")
	}

	return nil
}

func validatePostgresSSLMode(environment, sslMode string) error {
	mode := strings.ToLower(strings.TrimSpace(sslMode))
	if mode == "" {
		return fmt.Errorf("POSTGRES_SSLMODE is required")
	}

	valid := map[string]struct{}{
		"disable":     {},
		"allow":       {},
		"prefer":      {},
		"require":     {},
		"verify-ca":   {},
		"verify-full": {},
	}
	if _, ok := valid[mode]; !ok {
		return fmt.Errorf("invalid POSTGRES_SSLMODE: %s", sslMode)
	}

	if strings.EqualFold(strings.TrimSpace(environment), "production") {
		switch mode {
		case "disable", "allow", "prefer":
			return fmt.Errorf("POSTGRES_SSLMODE=%s is not allowed in production; use require, verify-ca, or verify-full", sslMode)
		}
	}

	return nil
}

func validateAPISecretKey(environment, apiKey string) error {
	if !strings.EqualFold(strings.TrimSpace(environment), "production") {
		return nil
	}
	if strings.TrimSpace(apiKey) != "" {
		return nil
	}
	return fmt.Errorf("API_SECRET_KEY is required in production")
}
