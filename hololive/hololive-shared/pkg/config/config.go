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

// LoggingConfig: 애플리케이션 로그 설정 (레벨, 디렉토리, 로테이션 정책)
type LoggingConfig struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
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
	Enabled        bool    // 트레이싱 활성화 여부
	ServiceName    string  // 서비스 식별자 (ex "hololive-bot")
	ServiceVersion string  // 서비스 버전 (ex "1.0.0")
	Environment    string  // 배포 환경 (ex "production")
	OTLPEndpoint   string  // OTLP collector 주소 (ex "otel-collector:4317")
	OTLPInsecure   bool    // TLS 없이 연결 (내부망 전용)
	SampleRate     float64 // 샘플링 비율 (0.0 ~ 1.0)
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

	runtimeEnv := strings.TrimSpace(envutil.String("APP_ENV", envutil.String("OTEL_ENVIRONMENT", "production")))
	isProduction := strings.EqualFold(runtimeEnv, "production")
	corsAllowedOrigins, corsMissingInProduction := parseCORSAllowedOrigins(
		envutil.String("CORS_ALLOWED_ORIGINS", ""),
		isProduction,
	)

	return webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction
}

func buildConfig(webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) *Config {
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
			SelfUser:         stringutil.TrimSpace(envutil.String("BOT_SELF_USER", "iris")),
			IngestionEnabled: envutil.Bool("BOT_INGESTION_ENABLED", true),
			AdminEnabled:     envutil.Bool("BOT_ADMIN_ENABLED", true),
		},
		Services: ServicesConfig{
			LLMServerHealthURL:      envutil.String("SERVICES_LLM_SERVER_HEALTH_URL", ""),
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
		Version: stringutil.TrimSpace(envutil.String("APP_VERSION", "1.1.0-go")),
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

func loadValkeyConfig() ValkeyConfig {
	return ValkeyConfig{
		Host:       envutil.String("CACHE_HOST", "localhost"),
		Port:       envutil.Int("CACHE_PORT", 6379),
		Password:   envutil.String("CACHE_PASSWORD", ""),
		DB:         envutil.Int("CACHE_DB", 0),
		SocketPath: envutil.String("CACHE_SOCKET_PATH", ""),
	}
}

func loadPostgresConfig() PostgresConfig {
	return PostgresConfig{
		Host:              envutil.String("POSTGRES_HOST", constants.DatabaseDefaults.Host),
		Port:              envutil.Int("POSTGRES_PORT", constants.DatabaseDefaults.Port),
		SocketPath:        envutil.String("POSTGRES_SOCKET_PATH", ""),
		User:              envutil.String("POSTGRES_USER", constants.DatabaseDefaults.User),
		Password:          envutil.String("POSTGRES_PASSWORD", constants.DatabaseDefaults.Password),
		Database:          envutil.String("POSTGRES_DB", constants.DatabaseDefaults.Database),
		SSLMode:           envutil.String("POSTGRES_SSLMODE", "require"),
		QueryExecMode:     envutil.String("POSTGRES_QUERY_EXEC_MODE", "cache_statement"),
		PoolMinConns:      envutil.Int("POSTGRES_POOL_MIN_CONNS", constants.DatabaseConfig.MaxIdleConns),
		PoolMaxConns:      envutil.Int("POSTGRES_POOL_MAX_CONNS", constants.DatabaseConfig.MaxOpenConns),
		PoolMaxIdleConns:  envutil.Int("POSTGRES_POOL_MAX_IDLE_CONNS", constants.DatabaseConfig.MaxIdleConns),
		AutoPrepareSchema: envutil.Bool("POSTGRES_AUTO_PREPARE_SCHEMA", true),
	}
}

func loadCliproxyConfig() CliproxyConfig {
	return CliproxyConfig{
		BaseURL:         envutil.String("CLIPROXY_BASE_URL", "https://cliproxy.capu.blog/v1"),
		APIKey:          envutil.String("CLIPROXY_API_KEY", ""),
		Model:           envutil.String("CLIPROXY_MODEL", "gpt-5.3-codex"),
		Enabled:         envutil.Bool("CLIPROXY_ENABLED", false),
		ReasoningEffort: envutil.String("CLIPROXY_REASONING_EFFORT", "high"),
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
	reviewTimeout := envutil.Int(prefix+"_REVIEW_TIMEOUT_SEC", 30)
	if reviewTimeout < 5 {
		reviewTimeout = 30
	}
	adjudicateTimeout := envutil.Int(prefix+"_ADJUDICATE_TIMEOUT_SEC", 45)
	if adjudicateTimeout < 5 {
		adjudicateTimeout = 45
	}

	return ConsensusLLMConfig{
		Enabled:           envutil.Bool(prefix+"_CONSENSUS_ENABLED", false),
		Confidence:        clampConfidence(envutil.Float(prefix+"_CONSENSUS_CONFIDENCE", 0.85)),
		ReviewerModel:     envutil.String(prefix+"_REVIEWER_MODEL", ""),
		AdjudicatorModel:  envutil.String(prefix+"_ADJUDICATOR_MODEL", ""),
		ReviewTimeout:     reviewTimeout,
		AdjudicateTimeout: adjudicateTimeout,
	}
}

func loadLLMConfig() LLMConfig {
	return LLMConfig{
		MemberNewsModel:       envutil.String("MEMBER_NEWS_LLM_MODEL", ""),
		MemberNewsTemperature: envutil.Float("MEMBER_NEWS_TEMPERATURE", 0), // GPT-5: temperature=1.0만 지원, 0=미설정(SDK 기본값)
		MemberNews:            loadConsensusLLMConfig("MEMBER_NEWS"),
		MajorEvent:            loadConsensusLLMConfig("MAJOREVENT"),
	}
}

func loadExaConfig() ExaConfig {
	return ExaConfig{
		Endpoint: envutil.String("EXA_MCP_ENDPOINT", "https://mcp.exa.ai/mcp"),
		APIKey:   envutil.String("EXA_API_KEY", ""),
		Enabled:  envutil.Bool("EXA_ENABLED", false),
	}
}

func loadTelemetryConfig() TelemetryConfig {
	return TelemetryConfig{
		Enabled:        envutil.Bool("OTEL_ENABLED", false),
		ServiceName:    envutil.String("OTEL_SERVICE_NAME", "hololive-bot"),
		ServiceVersion: envutil.String("OTEL_SERVICE_VERSION", "1.0.0"),
		Environment:    envutil.String("OTEL_ENVIRONMENT", "production"),
		OTLPEndpoint:   envutil.String("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317"),
		OTLPInsecure:   envutil.Bool("OTEL_EXPORTER_OTLP_INSECURE", false),
		SampleRate:     envutil.Float("OTEL_SAMPLE_RATE", 1.0),
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
