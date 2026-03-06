package config

import "time"

// ServerConfig: HTTP 서버 바인딩 포트 및 API 인증 설정
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

// LoggingConfig: 애플리케이션 로그 설정
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
	LLMServerHealthURL      string // legacy key alias for llm-scheduler health URL
	LLMSchedulerHealthURL   string // llm-scheduler health URL
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

// WebhookConfig: Iris 웹훅 워커 및 큐 설정
type WebhookConfig struct {
	WorkerCount    int
	QueueSize      int
	EnqueueTimeout time.Duration
	HandlerTimeout time.Duration
	RequireHTTP2   bool
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
