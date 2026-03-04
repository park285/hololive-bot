package config

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	sharedirisx "github.com/park285/llm-kakao-bots/shared-go/pkg/irisx"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

const maxHolodexAPIKeySlots = 5

type valkeyEnvConfig struct {
	Host       string `envconfig:"CACHE_HOST" default:"localhost"`
	Port       string `envconfig:"CACHE_PORT" default:"6379"`
	Password   string `envconfig:"CACHE_PASSWORD"`
	DB         string `envconfig:"CACHE_DB" default:"0"`
	SocketPath string `envconfig:"CACHE_SOCKET_PATH"`
}

type postgresEnvConfig struct {
	Host              string `envconfig:"POSTGRES_HOST"`
	Port              string `envconfig:"POSTGRES_PORT"`
	SocketPath        string `envconfig:"POSTGRES_SOCKET_PATH"`
	User              string `envconfig:"POSTGRES_USER"`
	Password          string `envconfig:"POSTGRES_PASSWORD"`
	Database          string `envconfig:"POSTGRES_DB"`
	SSLMode           string `envconfig:"POSTGRES_SSLMODE" default:"require"`
	QueryExecMode     string `envconfig:"POSTGRES_QUERY_EXEC_MODE" default:"cache_statement"`
	PoolMinConns      string `envconfig:"POSTGRES_POOL_MIN_CONNS"`
	PoolMaxConns      string `envconfig:"POSTGRES_POOL_MAX_CONNS"`
	PoolMaxIdleConns  string `envconfig:"POSTGRES_POOL_MAX_IDLE_CONNS"`
	AutoPrepareSchema string `envconfig:"POSTGRES_AUTO_PREPARE_SCHEMA" default:"true"`
}

type telemetryEnvConfig struct {
	Enabled                  string `envconfig:"OTEL_ENABLED" default:"false"`
	MetricsEnabled           string `envconfig:"OTEL_METRICS_ENABLED" default:"false"`
	MetricsExportIntervalSec string `envconfig:"OTEL_METRICS_EXPORT_INTERVAL_SECONDS" default:"30"`
	ServiceName              string `envconfig:"OTEL_SERVICE_NAME" default:"hololive-bot"`
	ServiceVersion           string `envconfig:"OTEL_SERVICE_VERSION" default:"1.0.0"`
	Environment              string `envconfig:"OTEL_ENVIRONMENT" default:"production"`
	OTLPEndpoint             string `envconfig:"OTEL_EXPORTER_OTLP_ENDPOINT" default:"otel-collector:4317"`
	OTLPInsecure             string `envconfig:"OTEL_EXPORTER_OTLP_INSECURE" default:"false"`
	SampleRate               string `envconfig:"OTEL_SAMPLE_RATE" default:"1.0"`
}

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

type cliproxyEnvConfig struct {
	BaseURL         string `envconfig:"CLIPROXY_BASE_URL" default:"https://cliproxy.capu.blog/v1"`
	APIKey          string `envconfig:"CLIPROXY_API_KEY"`
	Model           string `envconfig:"CLIPROXY_MODEL" default:"gpt-5.3-codex"`
	Enabled         string `envconfig:"CLIPROXY_ENABLED" default:"false"`
	ReasoningEffort string `envconfig:"CLIPROXY_REASONING_EFFORT" default:"high"`
}

type llmEnvConfig struct {
	MemberNewsModel       string `envconfig:"MEMBER_NEWS_LLM_MODEL"`
	MemberNewsTemperature string `envconfig:"MEMBER_NEWS_TEMPERATURE" default:"0"`
}

type consensusLLMEnvConfig struct {
	ConsensusEnabled     string `envconfig:"CONSENSUS_ENABLED" default:"false"`
	ConsensusConfidence  string `envconfig:"CONSENSUS_CONFIDENCE" default:"0.85"`
	ReviewerModel        string `envconfig:"REVIEWER_MODEL"`
	AdjudicatorModel     string `envconfig:"ADJUDICATOR_MODEL"`
	ReviewTimeoutSec     string `envconfig:"REVIEW_TIMEOUT_SEC" default:"30"`
	AdjudicateTimeoutSec string `envconfig:"ADJUDICATE_TIMEOUT_SEC" default:"45"`
}

type exaEnvConfig struct {
	Endpoint string `envconfig:"EXA_MCP_ENDPOINT" default:"https://mcp.exa.ai/mcp"`
	APIKey   string `envconfig:"EXA_API_KEY"`
	Enabled  string `envconfig:"EXA_ENABLED" default:"false"`
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

// CliproxyConfig: Cliproxy API 직접 호출 설정 (이벤트 요약용)
type CliproxyConfig struct {
	BaseURL         string
	APIKey          string
	Model           string
	Enabled         bool
	ReasoningEffort string // reasoning 모델용 사고 깊이 (high, xhigh 등)
}

// ConsensusLLMConfig: dual-agent review(consensus) 공통 설정
type ConsensusLLMConfig struct {
	Enabled           bool
	Confidence        float64
	ReviewerModel     string
	AdjudicatorModel  string
	ReviewTimeout     int
	AdjudicateTimeout int
}

// LLMConfig: LLM 서비스별 모델 설정
type LLMConfig struct {
	MemberNewsModel       string  // 최종 모델명 (dual-read 해결 완료, 빈 문자열이면 Cliproxy.Model 사용)
	MemberNewsTemperature float64 // MEMBER_NEWS_TEMPERATURE

	MemberNews ConsensusLLMConfig // MEMBER_NEWS_CONSENSUS_* 환경변수 그룹
	MajorEvent ConsensusLLMConfig // MAJOREVENT_CONSENSUS_* 환경변수 그룹
}

// ExaConfig: Exa MCP 검색 설정 (이벤트 요약용)
type ExaConfig struct {
	Endpoint string
	APIKey   string
	Enabled  bool
}

// IrisConfig: Iris 웹훅 서버 연결 및 메시지 전송 관련 설정
type IrisConfig struct {
	BaseURL                   string
	WebhookToken              string // env: IRIS_WEBHOOK_TOKEN
	BotToken                  string // env: IRIS_BOT_TOKEN
	HTTPTimeout               time.Duration
	HTTPDialTimeout           time.Duration
	HTTPResponseHeaderTimeout time.Duration
}

// ServerConfig: HTTP 서버 설정
type ServerConfig struct {
	Port   int
	APIKey string // API 인증용 시크릿 키 (X-API-Key 헤더로 검증)
}

// KakaoConfig: 카카오톡 채팅방 허용 목록 및 접근 제어(ACL) 설정
type KakaoConfig struct {
	Rooms      []string
	ACLEnabled bool

	mu sync.RWMutex
}

// SnapshotACL: 현재 ACL 설정 상태(활성화 여부 및 허용된 방 목록)의 스냅샷을 반환합니다.
// Thread-safe하게 읽기 락을 사용한다.
func (c *KakaoConfig) SnapshotACL() (enabled bool, rooms []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rooms = append([]string(nil), c.Rooms...)
	return c.ACLEnabled, rooms
}

// SetACLEnabled: ACL(접근 제어) 기능의 활성화 여부를 '동적으로' 설정합니다.
func (c *KakaoConfig) SetACLEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ACLEnabled = enabled
}

// AddRoom: 허용 목록에 새로운 채팅방을 추가한다. 이미 존재하면 false를 반환합니다.
func (c *KakaoConfig) AddRoom(room string) bool {
	room = stringutil.TrimSpace(room)
	if room == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if slices.Contains(c.Rooms, room) {
		return false
	}

	c.Rooms = append(c.Rooms, room)
	return true
}

// RemoveRoom: 허용 목록에서 특정 채팅방을 제거합니다.
func (c *KakaoConfig) RemoveRoom(room string) bool {
	room = stringutil.TrimSpace(room)
	if room == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	removed := false
	rooms := make([]string, 0, len(c.Rooms))
	for _, existing := range c.Rooms {
		if existing == room {
			removed = true
			continue
		}
		rooms = append(rooms, existing)
	}

	c.Rooms = rooms
	return removed
}

// IsRoomAllowed: 해당 채팅방(chatID)이 봇 사용이 허용된 곳인지 확인합니다.
// ACL이 비활성화되어 있으면 모든 방을 허용한다.
func (c *KakaoConfig) IsRoomAllowed(roomName, chatID string) bool {
	chatID = stringutil.TrimSpace(chatID)

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.ACLEnabled {
		return true
	}

	// chatID 기반으로만 검증 (roomName은 참고용으로만 유지)
	if chatID == "" {
		return false // chatID가 없으면 거부
	}

	return slices.Contains(c.Rooms, chatID)
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

// ValkeyConfig: 데이터 캐싱 용도의 Redis(Valkey) 연결 설정
type ValkeyConfig struct {
	Host       string
	Port       int
	Password   string
	DB         int
	SocketPath string // UDS 경로 (비어있으면 TCP 사용)
}

// PostgresConfig: 메인 데이터베이스(PostgreSQL) 연결 설정
type PostgresConfig struct {
	Host              string
	Port              int
	SocketPath        string // UDS 경로 (비어있으면 TCP 사용)
	User              string
	Password          string
	Database          string
	SSLMode           string
	QueryExecMode     string
	PoolMinConns      int
	PoolMaxConns      int
	PoolMaxIdleConns  int
	AutoPrepareSchema bool
}

// NotificationConfig: 방송 알림 스케줄링(미리 알림 시간, 체크 주기) 설정
type NotificationConfig struct {
	AdvanceMinutes []int
	CheckInterval  time.Duration
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

// TelemetryConfig: OpenTelemetry 분산 추적 설정
type TelemetryConfig struct {
	Enabled               bool          // 트레이싱 활성화 여부
	MetricsEnabled        bool          // OTel metrics export 활성화 여부 (Prometheus와 병행 가능)
	MetricsExportInterval time.Duration // OTel metrics export 주기
	ServiceName           string        // 서비스 식별자 (ex "hololive-bot")
	ServiceVersion        string        // 서비스 버전 (ex "1.0.0")
	Environment           string        // 배포 환경 (ex "production")
	OTLPEndpoint          string        // OTLP collector 주소 (ex "otel-collector:4317")
	OTLPInsecure          bool          // TLS 없이 연결 (내부망 전용)
	SampleRate            float64       // 샘플링 비율 (0.0 ~ 1.0)
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

func loadValkeyConfig() ValkeyConfig {
	var raw valkeyEnvConfig
	_ = envconfig.Process("", &raw)

	return ValkeyConfig{
		Host:       parseStringWithDefault(raw.Host, "localhost"),
		Port:       parseIntWithDefault(raw.Port, 6379),
		Password:   raw.Password,
		DB:         parseIntWithDefault(raw.DB, 0),
		SocketPath: strings.TrimSpace(raw.SocketPath),
	}
}

func loadPostgresConfig() PostgresConfig {
	var raw postgresEnvConfig
	_ = envconfig.Process("", &raw)

	host := strings.TrimSpace(raw.Host)
	if host == "" {
		host = constants.DatabaseDefaults.Host
	}
	user := strings.TrimSpace(raw.User)
	if user == "" {
		user = constants.DatabaseDefaults.User
	}
	password := raw.Password
	if strings.TrimSpace(password) == "" {
		password = constants.DatabaseDefaults.Password
	}
	database := strings.TrimSpace(raw.Database)
	if database == "" {
		database = constants.DatabaseDefaults.Database
	}

	return PostgresConfig{
		Host:              host,
		Port:              parseIntWithDefault(raw.Port, constants.DatabaseDefaults.Port),
		SocketPath:        strings.TrimSpace(raw.SocketPath),
		User:              user,
		Password:          password,
		Database:          database,
		SSLMode:           parseStringWithDefault(raw.SSLMode, "require"),
		QueryExecMode:     parseStringWithDefault(raw.QueryExecMode, "cache_statement"),
		PoolMinConns:      parseIntWithDefault(raw.PoolMinConns, constants.DatabaseConfig.MaxIdleConns),
		PoolMaxConns:      parseIntWithDefault(raw.PoolMaxConns, constants.DatabaseConfig.MaxOpenConns),
		PoolMaxIdleConns:  parseIntWithDefault(raw.PoolMaxIdleConns, constants.DatabaseConfig.MaxIdleConns),
		AutoPrepareSchema: parseBoolWithDefault(raw.AutoPrepareSchema, true),
	}
}

func loadCliproxyConfig() CliproxyConfig {
	var raw cliproxyEnvConfig
	_ = envconfig.Process("", &raw)

	return CliproxyConfig{
		BaseURL:         parseStringWithDefault(raw.BaseURL, "https://cliproxy.capu.blog/v1"),
		APIKey:          strings.TrimSpace(raw.APIKey),
		Model:           parseStringWithDefault(raw.Model, "gpt-5.3-codex"),
		Enabled:         parseBoolWithDefault(raw.Enabled, false),
		ReasoningEffort: parseStringWithDefault(raw.ReasoningEffort, "high"),
	}
}

// clampConfidence: confidence 값을 [0, 1] 범위로 정규화한다.
// NaN/Inf 입력 시 기본값(0.85)을 반환한다.
func clampConfidence(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0.85
	}
	if v < 0 {
		return 0.0
	}
	if v > 1 {
		return 1.0
	}
	return v
}

// loadConsensusLLMConfig: prefix 기반 환경변수에서 ConsensusLLMConfig를 로드한다.
// prefix 예: "MEMBER_NEWS" -> MEMBER_NEWS_CONSENSUS_ENABLED, MEMBER_NEWS_CONSENSUS_CONFIDENCE, ...
func loadConsensusLLMConfig(prefix string) ConsensusLLMConfig {
	var raw consensusLLMEnvConfig
	_ = envconfig.Process(prefix, &raw)

	reviewTimeout := parseIntWithDefault(raw.ReviewTimeoutSec, 30)
	if reviewTimeout < 5 {
		reviewTimeout = 30
	}
	adjudicateTimeout := parseIntWithDefault(raw.AdjudicateTimeoutSec, 45)
	if adjudicateTimeout < 5 {
		adjudicateTimeout = 45
	}

	return ConsensusLLMConfig{
		Enabled:           parseBoolWithDefault(raw.ConsensusEnabled, false),
		Confidence:        clampConfidence(parseFloatWithDefault(raw.ConsensusConfidence, 0.85)),
		ReviewerModel:     strings.TrimSpace(raw.ReviewerModel),
		AdjudicatorModel:  strings.TrimSpace(raw.AdjudicatorModel),
		ReviewTimeout:     reviewTimeout,
		AdjudicateTimeout: adjudicateTimeout,
	}
}

func loadLLMConfig() LLMConfig {
	var raw llmEnvConfig
	_ = envconfig.Process("", &raw)

	return LLMConfig{
		MemberNewsModel:       strings.TrimSpace(raw.MemberNewsModel),
		MemberNewsTemperature: parseFloatWithDefault(raw.MemberNewsTemperature, 0), // GPT-5: temperature=1.0만 지원, 0=미설정(SDK 기본값)
		MemberNews:            loadConsensusLLMConfig("MEMBER_NEWS"),
		MajorEvent:            loadConsensusLLMConfig("MAJOREVENT"),
	}
}

func loadExaConfig() ExaConfig {
	var raw exaEnvConfig
	_ = envconfig.Process("", &raw)

	return ExaConfig{
		Endpoint: parseStringWithDefault(raw.Endpoint, "https://mcp.exa.ai/mcp"),
		APIKey:   strings.TrimSpace(raw.APIKey),
		Enabled:  parseBoolWithDefault(raw.Enabled, false),
	}
}

func loadTelemetryConfig() TelemetryConfig {
	var raw telemetryEnvConfig
	_ = envconfig.Process("", &raw)

	metricsExportIntervalSeconds := parseIntWithDefault(raw.MetricsExportIntervalSec, 30)
	if metricsExportIntervalSeconds <= 0 {
		metricsExportIntervalSeconds = 30
	}

	return TelemetryConfig{
		Enabled:               parseBoolWithDefault(raw.Enabled, false),
		MetricsEnabled:        parseBoolWithDefault(raw.MetricsEnabled, false),
		MetricsExportInterval: time.Duration(metricsExportIntervalSeconds) * time.Second,
		ServiceName:           parseStringWithDefault(raw.ServiceName, "hololive-bot"),
		ServiceVersion:        parseStringWithDefault(raw.ServiceVersion, "1.0.0"),
		Environment:           parseStringWithDefault(raw.Environment, "production"),
		OTLPEndpoint:          parseStringWithDefault(raw.OTLPEndpoint, "otel-collector:4317"),
		OTLPInsecure:          parseBoolWithDefault(raw.OTLPInsecure, false),
		SampleRate:            parseFloatWithDefault(raw.SampleRate, 1.0),
	}
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

func parseStringWithDefault(value, def string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return def
	}
	return trimmed
}

func parseIntWithDefault(value string, def int) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return def
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return def
	}
	return parsed
}

func parseFloatWithDefault(value string, def float64) float64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return def
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return def
	}
	return parsed
}

func parseBoolWithDefault(value string, def bool) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return def
	}
	return trimmed == "true" || trimmed == "1" || trimmed == "yes" || trimmed == "y"
}

func parseCommaSeparated(value string) []string {
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := stringutil.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func parseIntList(value string) []int {
	if value == "" {
		return []int{}
	}
	parts := strings.Split(value, ",")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		if trimmed := stringutil.TrimSpace(part); trimmed != "" {
			if intVal, err := strconv.Atoi(trimmed); err == nil {
				result = append(result, intVal)
			}
		}
	}
	return result
}

func collectAPIKeys(prefix string) []string {
	keys := make([]string, 0)
	seen := make(map[string]struct{})

	addKey := func(raw string) {
		trimmed := stringutil.TrimSpace(raw)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		keys = append(keys, trimmed)
	}

	for i := 1; i <= maxHolodexAPIKeySlots; i++ {
		envKey := fmt.Sprintf("%s%d", prefix, i)
		addKey(os.Getenv(envKey))
	}

	if base := strings.TrimSuffix(prefix, "_"); base != "" {
		if bulk := os.Getenv(base + "S"); bulk != "" {
			parts := strings.SplitSeq(bulk, ",")
			for part := range parts {
				addKey(part)
			}
		}
	}

	return keys
}

func parseCORSAllowedOrigins(rawOrigins string, isProduction bool) ([]string, bool) {
	origins := parseCommaSeparated(rawOrigins)
	if !isProduction {
		if len(origins) == 0 {
			return []string{"http://localhost:5173"}, false
		}
		return origins, false
	}

	filtered := make([]string, 0, len(origins))
	for _, origin := range origins {
		if origin == "*" {
			continue
		}
		if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "https://localhost") {
			continue
		}
		filtered = append(filtered, origin)
	}
	return filtered, len(filtered) == 0
}
