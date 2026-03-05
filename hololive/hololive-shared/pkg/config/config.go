package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	sharedirisx "github.com/park285/llm-kakao-bots/shared-go/pkg/irisx"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

const maxHolodexAPIKeySlots = 5

type appCoreEnvConfig struct {
	IrisBaseURL                  string `envconfig:"IRIS_BASE_URL" default:"http://localhost:3000"`
	IrisHTTPTimeoutSeconds       string `envconfig:"IRIS_HTTP_TIMEOUT_SECONDS" default:"10"`
	IrisHTTPDialTimeoutSeconds   string `envconfig:"IRIS_HTTP_DIAL_TIMEOUT_SECONDS" default:"3"`
	IrisHTTPRespHeaderTimeoutSec string `envconfig:"IRIS_HTTP_RESP_HEADER_TIMEOUT_SECONDS" default:"5"`

	ServerPort   string `envconfig:"SERVER_PORT" default:"30001"`
	APISecretKey string `envconfig:"API_SECRET_KEY"`

	KakaoRooms      string `envconfig:"KAKAO_ROOMS" default:"홀로라이브 알림방"`
	KakaoACLEnabled string `envconfig:"KAKAO_ACL_ENABLED" default:"true"`

	HolodexBaseURL string `envconfig:"HOLODEX_BASE_URL"`

	YouTubeAPIKey              string `envconfig:"YOUTUBE_API_KEY"`
	YouTubeEnableQuotaBuilding string `envconfig:"YOUTUBE_ENABLE_QUOTA_BUILDING" default:"false"`

	NotificationAdvanceMinutes string `envconfig:"NOTIFICATION_ADVANCE_MINUTES" default:"5"`
	CheckIntervalSeconds       string `envconfig:"CHECK_INTERVAL_SECONDS" default:"60"`

	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`

	BotPrefix           string `envconfig:"BOT_PREFIX" default:"!"`
	BotSelfUser         string `envconfig:"BOT_SELF_USER" default:"iris"`
	BotIngestionEnabled string `envconfig:"BOT_INGESTION_ENABLED" default:"true"`
	BotAdminEnabled     string `envconfig:"BOT_ADMIN_ENABLED" default:"true"`

	ServicesLLMServerHealthURL      string `envconfig:"SERVICES_LLM_SERVER_HEALTH_URL"`
	ServicesGameBotTwentyQHealthURL string `envconfig:"SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL"`
	ServicesGameBotTurtleHealthURL  string `envconfig:"SERVICES_GAME_BOT_TURTLE_HEALTH_URL"`

	ScraperProxyEnabled string `envconfig:"SCRAPER_PROXY_ENABLED" default:"false"`
	ScraperProxyURL     string `envconfig:"SCRAPER_PROXY_URL"`

	WebhookWorkerCount       string `envconfig:"WEBHOOK_WORKER_COUNT" default:"16"`
	WebhookQueueSize         string `envconfig:"WEBHOOK_QUEUE_SIZE" default:"1000"`
	WebhookEnqueueTimeoutMS  string `envconfig:"WEBHOOK_ENQUEUE_TIMEOUT_MS" default:"50"`
	WebhookHandlerTimeoutSec string `envconfig:"WEBHOOK_HANDLER_TIMEOUT_SECONDS" default:"30"`

	ChzzkClientID     string `envconfig:"CHZZK_CLIENT_ID"`
	ChzzkClientSecret string `envconfig:"CHZZK_CLIENT_SECRET"`

	TwitchClientID     string `envconfig:"TWITCH_CLIENT_ID"`
	TwitchClientSecret string `envconfig:"TWITCH_CLIENT_SECRET"`

	AlarmDispatcherURL string `envconfig:"ALARM_DISPATCHER_URL"`
	LLMSchedulerURL    string `envconfig:"LLM_SCHEDULER_INTERNAL_URL"`
	AppVersion         string `envconfig:"APP_VERSION" default:"1.1.0-go"`
	CORSEnforce        string `envconfig:"CORS_ENFORCE" default:"false"`
}

type runtimeTokenEnvConfig struct {
	IrisSharedToken  string `envconfig:"IRIS_SHARED_TOKEN"`
	IrisWebhookToken string `envconfig:"IRIS_WEBHOOK_TOKEN"`
	IrisBotToken     string `envconfig:"IRIS_BOT_TOKEN"`

	AppEnv          string `envconfig:"APP_ENV"`
	OTELEnvironment string `envconfig:"OTEL_ENVIRONMENT" default:"production"`

	CORSAllowedOrigins string `envconfig:"CORS_ALLOWED_ORIGINS"`
}

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

// ServerConfig: HTTP 서버 설정
type ServerConfig struct {
	Port   int
	APIKey string // API 인증용 시크릿 키 (X-API-Key 헤더로 검증)
}

// HolodexConfig: Holodex API 키 및 호출 관련 설정
type HolodexConfig struct {
	BaseURL string
	APIKeys []string
}

// YouTubeConfig: YouTube Data API 키 및 Quota 관리 설정
type YouTubeConfig struct {
	APIKey              string
	EnableQuotaBuilding bool
}

// LoggingConfig: 애플리케이션 로그 설정 (레벨)
type LoggingConfig struct {
	Level string
}

// BotConfig: 봇의 기본 동작(명령어 접두사, 자기 자신 식별자) 설정
type BotConfig struct {
	Prefix           string
	SelfUser         string
	IngestionEnabled bool
	AdminEnabled     bool
}

// ServicesConfig: 외부 Go 서비스 연결 설정 (goroutine 통합 모니터링용)
type ServicesConfig struct {
	LLMServerHealthURL      string // mcp-llm-server-go health URL
	GameBotTwentyQHealthURL string // game-bot-go twentyq health URL
	GameBotTurtleHealthURL  string // game-bot-go turtlesoup health URL
}

// ScraperConfig: YouTube 스크래퍼 프록시 설정 (SOCKS5)
type ScraperConfig struct {
	ProxyEnabled bool   // 프록시 사용 여부
	ProxyURL     string // SOCKS5 프록시 URL (예: socks5://user:pass@host:1080)
}

// CORSConfig: CORS 허용 Origin 설정
type CORSConfig struct {
	AllowedOrigins      []string
	Enforce             bool
	MissingInProduction bool
}

type WebhookConfig struct {
	WorkerCount    int
	QueueSize      int
	EnqueueTimeout time.Duration
	HandlerTimeout time.Duration
}

// ChzzkConfig: 치지직 Open API 설정 (Client 인증 방식)
type ChzzkConfig struct {
	ClientID     string
	ClientSecret string
}

// TwitchConfig: Twitch Helix API 설정 (Client Credentials 인증)
type TwitchConfig struct {
	ClientID     string
	ClientSecret string
}

// Load: .env 파일 및 환경 변수로부터 설정을 로드하고, 기본값을 적용하여 Config 객체를 생성합니다.
func Load() (*Config, error) {
	_ = godotenv.Load()

	webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction := loadRuntimeTokensAndCORS()
	cfg, err := buildConfig(webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction)
	if err != nil {
		return nil, fmt.Errorf("build config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func loadRuntimeTokensAndCORS() (string, string, []string, bool) {
	var raw runtimeTokenEnvConfig
	_ = envconfig.Process("", &raw)

	sharedIrisToken := strings.TrimSpace(raw.IrisSharedToken)
	webhookToken, botToken := sharedirisx.ResolveTokens(
		strings.TrimSpace(raw.IrisWebhookToken),
		strings.TrimSpace(raw.IrisBotToken),
		sharedIrisToken,
	)

	runtimeEnv := strings.TrimSpace(raw.AppEnv)
	if runtimeEnv == "" {
		runtimeEnv = parseStringWithDefault(raw.OTELEnvironment, "production")
	}
	isProduction := strings.EqualFold(runtimeEnv, "production")
	corsAllowedOrigins, corsMissingInProduction := parseCORSAllowedOrigins(
		strings.TrimSpace(raw.CORSAllowedOrigins),
		isProduction,
	)

	return webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction
}

func buildConfig(webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) (*Config, error) {
	var raw appCoreEnvConfig
	if err := envconfig.Process("", &raw); err != nil {
		return nil, fmt.Errorf("process env: %w", err)
	}

	holodexBaseURL := parseStringWithDefault(raw.HolodexBaseURL, constants.APIConfig.HolodexBaseURL)

	return &Config{
		Iris: IrisConfig{
			BaseURL:                   parseStringWithDefault(raw.IrisBaseURL, "http://localhost:3000"),
			WebhookToken:              webhookToken,
			BotToken:                  botToken,
			HTTPTimeout:               time.Duration(parseIntWithDefault(raw.IrisHTTPTimeoutSeconds, 10)) * time.Second,
			HTTPDialTimeout:           time.Duration(parseIntWithDefault(raw.IrisHTTPDialTimeoutSeconds, 3)) * time.Second,
			HTTPResponseHeaderTimeout: time.Duration(parseIntWithDefault(raw.IrisHTTPRespHeaderTimeoutSec, 5)) * time.Second,
		},
		Server: ServerConfig{
			Port:   parseIntWithDefault(raw.ServerPort, 30001),
			APIKey: strings.TrimSpace(raw.APISecretKey),
		},
		Kakao: KakaoConfig{
			Rooms:      parseCommaSeparated(parseStringWithDefault(raw.KakaoRooms, "홀로라이브 알림방")),
			ACLEnabled: parseBoolWithDefault(raw.KakaoACLEnabled, true),
		},
		Holodex: HolodexConfig{
			BaseURL: holodexBaseURL,
			APIKeys: collectAPIKeys("HOLODEX_API_KEY_"),
		},
		YouTube: YouTubeConfig{
			APIKey:              strings.TrimSpace(raw.YouTubeAPIKey),
			EnableQuotaBuilding: parseBoolWithDefault(raw.YouTubeEnableQuotaBuilding, false),
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Notification: NotificationConfig{
			AdvanceMinutes: parseIntList(parseStringWithDefault(raw.NotificationAdvanceMinutes, "5")),
			CheckInterval:  time.Duration(parseIntWithDefault(raw.CheckIntervalSeconds, 60)) * time.Second,
		},
		Logging: LoggingConfig{
			Level: parseStringWithDefault(raw.LogLevel, "info"),
		},
		Bot: BotConfig{
			Prefix:           parseStringWithDefault(raw.BotPrefix, "!"),
			SelfUser:         stringutil.TrimSpace(parseStringWithDefault(raw.BotSelfUser, "iris")),
			IngestionEnabled: parseBoolWithDefault(raw.BotIngestionEnabled, true),
			AdminEnabled:     parseBoolWithDefault(raw.BotAdminEnabled, true),
		},
		Services: ServicesConfig{
			LLMServerHealthURL:      strings.TrimSpace(raw.ServicesLLMServerHealthURL),
			GameBotTwentyQHealthURL: strings.TrimSpace(raw.ServicesGameBotTwentyQHealthURL),
			GameBotTurtleHealthURL:  strings.TrimSpace(raw.ServicesGameBotTurtleHealthURL),
		},
		Telemetry: loadTelemetryConfig(),
		Scraper: ScraperConfig{
			ProxyEnabled: parseBoolWithDefault(raw.ScraperProxyEnabled, false),
			ProxyURL:     strings.TrimSpace(raw.ScraperProxyURL),
		},
		Webhook: WebhookConfig{
			WorkerCount:    parseIntWithDefault(raw.WebhookWorkerCount, 16),
			QueueSize:      parseIntWithDefault(raw.WebhookQueueSize, 1000),
			EnqueueTimeout: time.Duration(parseIntWithDefault(raw.WebhookEnqueueTimeoutMS, 50)) * time.Millisecond,
			HandlerTimeout: time.Duration(parseIntWithDefault(raw.WebhookHandlerTimeoutSec, 30)) * time.Second,
		},
		Chzzk: ChzzkConfig{
			ClientID:     strings.TrimSpace(raw.ChzzkClientID),
			ClientSecret: strings.TrimSpace(raw.ChzzkClientSecret),
		},
		Twitch: TwitchConfig{
			ClientID:     strings.TrimSpace(raw.TwitchClientID),
			ClientSecret: strings.TrimSpace(raw.TwitchClientSecret),
		},
		Cliproxy:           loadCliproxyConfig(),
		LLM:                loadLLMConfig(),
		Exa:                loadExaConfig(),
		AlarmDispatcherURL: strings.TrimSpace(raw.AlarmDispatcherURL),
		LLMSchedulerURL:    strings.TrimSpace(raw.LLMSchedulerURL),
		CORS: CORSConfig{
			AllowedOrigins:      corsAllowedOrigins,
			Enforce:             parseBoolWithDefault(raw.CORSEnforce, false),
			MissingInProduction: corsMissingInProduction,
		},
		Version: stringutil.TrimSpace(parseStringWithDefault(raw.AppVersion, "1.1.0-go")),
	}, nil
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
