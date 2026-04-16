// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	sharedenv "github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type Config struct {
	Iris            IrisConfig
	Server          ServerConfig
	Kakao           KakaoConfig
	Holodex         HolodexConfig
	YouTube         YouTubeConfig
	Ingestion       IngestionConfig
	Chzzk           ChzzkConfig // 치지직 Open API 설정
	Twitch          TwitchConfig
	Valkey          ValkeyConfig
	Postgres        PostgresConfig
	Notification    NotificationConfig
	Logging         LoggingConfig
	Bot             BotConfig
	Services        ServicesConfig
	Environment     string
	Scraper         ScraperConfig // YouTube 스크래퍼 프록시 설정
	Webhook         WebhookConfig
	CORS            CORSConfig // CORS 설정
	Cliproxy        CliproxyConfig
	LLM             LLMConfig
	Exa             ExaConfig
	LLMSchedulerURL string // llm-scheduler 내부 API URL (bot이 구독/다이제스트 요청 시 사용)
	Version         string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction := loadRuntimeTokensAndCORS()
	cfg, err := buildConfig(webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction)
	if err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func loadRuntimeTokensAndCORS() (string, string, []string, bool) {
	webhookToken := strings.TrimSpace(sharedenv.String("IRIS_WEBHOOK_TOKEN", ""))
	botToken := strings.TrimSpace(sharedenv.String("IRIS_BOT_TOKEN", ""))

	runtimeEnv := loadAppEnvironment()
	isProduction := strings.EqualFold(runtimeEnv, "production")
	corsAllowedOrigins, corsMissingInProduction := parseCORSAllowedOrigins(
		sharedenv.String("CORS_ALLOWED_ORIGINS", ""),
		isProduction,
	)

	return webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction
}

//nolint:funlen // central environment-to-config assembly is intentionally kept in one place
func buildConfig(webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) (*Config, error) {
	llmSchedulerHealthURL := sharedenv.StringAny(
		"SERVICES_LLM_SCHEDULER_HEALTH_URL",
		"SERVICES_LLM_SERVER_HEALTH_URL",
	)
	publishedAtResolverDefaults := DefaultScraperPublishedAtResolverConfig()
	communityShortsBigBangCutoverAt, err := loadCommunityShortsBigBangCutoverAt()
	if err != nil {
		return nil, err
	}

	return &Config{
		Iris: IrisConfig{
			BaseURL:                   sharedenv.String("IRIS_BASE_URL", "http://localhost:3000"),
			WebhookToken:              webhookToken,
			BotToken:                  botToken,
			HTTPTimeout:               time.Duration(sharedenv.Int("IRIS_HTTP_TIMEOUT_SECONDS", 10)) * time.Second,
			HTTPDialTimeout:           time.Duration(sharedenv.Int("IRIS_HTTP_DIAL_TIMEOUT_SECONDS", 3)) * time.Second,
			HTTPResponseHeaderTimeout: time.Duration(sharedenv.Int("IRIS_HTTP_RESP_HEADER_TIMEOUT_SECONDS", 5)) * time.Second,
		},
		Server: ServerConfig{
			Port:   sharedenv.Int("SERVER_PORT", 30001),
			APIKey: sharedenv.String("API_SECRET_KEY", ""),
		},
		Kakao: KakaoConfig{
			Rooms:      parseCommaSeparated(sharedenv.String("KAKAO_ROOMS", "홀로라이브 알림방")),
			ACLEnabled: sharedenv.Bool("KAKAO_ACL_ENABLED", true),
			ACLMode:    sharedenv.String("KAKAO_ACL_MODE", "whitelist"),
		},
		Holodex: HolodexConfig{
			BaseURL: sharedenv.String("HOLODEX_BASE_URL", constants.APIConfig.HolodexBaseURL),
			APIKey:  resolveHolodexAPIKey(),
		},
		YouTube: YouTubeConfig{
			APIKey:              sharedenv.String("YOUTUBE_API_KEY", ""),
			EnableQuotaBuilding: sharedenv.Bool("YOUTUBE_ENABLE_QUOTA_BUILDING", false),
		},
		Ingestion: IngestionConfig{
			YouTubeEnabled:                  sharedenv.Bool("YOUTUBE_INGESTION_ENABLED", true),
			PhotoSyncEnabled:                sharedenv.Bool("PHOTO_SYNC_ENABLED", true),
			CommunityShortsBigBangEnabled:   sharedenv.Bool("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED", false),
			CommunityShortsBigBangCutoverAt: communityShortsBigBangCutoverAt,
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Notification: NotificationConfig{
			AdvanceMinutes: parseIntList(sharedenv.String("NOTIFICATION_ADVANCE_MINUTES", "5")),
			CheckInterval:  time.Duration(sharedenv.Int("CHECK_INTERVAL_SECONDS", 60)) * time.Second,
		},
		Logging: LoggingConfig{
			Level:      sharedenv.String("LOG_LEVEL", "info"),
			Dir:        sharedenv.String("LOG_DIR", ""),
			MaxSizeMB:  sharedenv.Int("LOG_MAX_SIZE_MB", 100),
			MaxBackups: sharedenv.Int("LOG_MAX_BACKUPS", 5),
			MaxAgeDays: sharedenv.Int("LOG_MAX_AGE_DAYS", 30),
			Compress:   sharedenv.Bool("LOG_COMPRESS", true),
		},
		Bot: BotConfig{
			Prefix:        sharedenv.String("BOT_PREFIX", "!"),
			SelfUser:      sharedenv.String("BOT_SELF_USER", "iris"),
			AdminEnabled:  sharedenv.Bool("BOT_ADMIN_ENABLED", true),
			MentionPrefix: sharedenv.String("BOT_MENTION_PREFIX", "#kapu봇"),
		},
		Services: ServicesConfig{
			LLMSchedulerHealthURL:   llmSchedulerHealthURL,
			GameBotTwentyQHealthURL: sharedenv.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
			GameBotTurtleHealthURL:  sharedenv.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
		},
		Environment: loadAppEnvironment(),
		Scraper: ScraperConfig{
			ProxyEnabled: sharedenv.Bool("SCRAPER_PROXY_ENABLED", false),
			ProxyURL:     sharedenv.String("SCRAPER_PROXY_URL", ""),
			WorkerCount: intAliasEnv([]string{
				"SCRAPER_SCHEDULER_WORKER_COUNT",
				"SCRAPER_WORKER_COUNT",
			}, DefaultScraperWorkerCount()),
			Poll: loadScraperPoll(),
			PublishedAtResolver: ScraperPublishedAtResolverConfig{
				Enabled:           sharedenv.Bool("SCRAPER_PUBLISHED_AT_RESOLVER_ENABLED", publishedAtResolverDefaults.Enabled),
				Interval:          time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_INTERVAL_SECONDS", int(publishedAtResolverDefaults.Interval/time.Second))) * time.Second,
				BatchSize:         sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_BATCH_SIZE", publishedAtResolverDefaults.BatchSize),
				MaxResolvePerRun:  sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RESOLVE_PER_RUN", publishedAtResolverDefaults.MaxResolvePerRun),
				MaxRunDuration:    time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS", int(publishedAtResolverDefaults.MaxRunDuration/time.Second))) * time.Second,
				ResolveTimeout:    time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_RESOLVE_TIMEOUT_SECONDS", int(publishedAtResolverDefaults.ResolveTimeout/time.Second))) * time.Second,
				MinDetectedAge:    time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MIN_DETECTED_AGE_SECONDS", int(publishedAtResolverDefaults.MinDetectedAge/time.Second))) * time.Second,
				FailureBackoffTTL: time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_FAILURE_BACKOFF_SECONDS", int(publishedAtResolverDefaults.FailureBackoffTTL/time.Second))) * time.Second,
			},
		},
		Webhook: WebhookConfig{
			WorkerCount:    sharedenv.Int("WEBHOOK_WORKER_COUNT", 16),
			QueueSize:      sharedenv.Int("WEBHOOK_QUEUE_SIZE", 1000),
			EnqueueTimeout: time.Duration(sharedenv.Int("WEBHOOK_ENQUEUE_TIMEOUT_MS", 50)) * time.Millisecond,
			HandlerTimeout: time.Duration(sharedenv.Int("WEBHOOK_HANDLER_TIMEOUT_SECONDS", 30)) * time.Second,
			RequireHTTP2:   sharedenv.Bool("WEBHOOK_REQUIRE_HTTP2", false),
		},
		Chzzk: ChzzkConfig{
			ClientID:     sharedenv.String("CHZZK_CLIENT_ID", ""),
			ClientSecret: sharedenv.String("CHZZK_CLIENT_SECRET", ""),
		},
		Twitch: TwitchConfig{
			ClientID:     sharedenv.String("TWITCH_CLIENT_ID", ""),
			ClientSecret: sharedenv.String("TWITCH_CLIENT_SECRET", ""),
		},
		Cliproxy:        loadCliproxyConfig(),
		LLM:             loadLLMConfig(),
		Exa:             loadExaConfig(),
		LLMSchedulerURL: sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		CORS: CORSConfig{
			AllowedOrigins:      corsAllowedOrigins,
			Enforce:             sharedenv.Bool("CORS_ENFORCE", false),
			MissingInProduction: corsMissingInProduction,
		},
		Version: sharedenv.String("APP_VERSION", "1.1.0-go"),
	}, nil
}

func (c *Config) Validate() error {
	if err := validateDeprecatedEnvUsage(); err != nil {
		return err
	}
	if c.Server.Port == 0 {
		return fmt.Errorf("SERVER_PORT is required")
	}
	if err := validateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return err
	}
	if len(c.Kakao.Rooms) == 0 {
		return fmt.Errorf("KAKAO_ROOMS is required")
	}
	if strings.TrimSpace(c.Iris.WebhookToken) == "" {
		return fmt.Errorf("IRIS_WEBHOOK_TOKEN is required")
	}
	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return fmt.Errorf("IRIS_BOT_TOKEN is required")
	}
	if strings.TrimSpace(c.Holodex.APIKey) == "" {
		return fmt.Errorf("HOLODEX_API_KEY is required")
	}
	if isPlaceholderAPIKey(c.YouTube.APIKey) {
		return fmt.Errorf("YOUTUBE_API_KEY uses placeholder value; set a real API key")
	}
	if err := validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	if err := validateScraperPublishedAtResolverConfig(c.Scraper.PublishedAtResolver); err != nil {
		return err
	}
	isProduction := strings.EqualFold(strings.TrimSpace(c.Environment), "production")
	if isProduction && c.CORS.Enforce && len(c.CORS.AllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true")
	}
	return nil
}

func validateScraperPublishedAtResolverConfig(cfg ScraperPublishedAtResolverConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Interval <= 0 {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_INTERVAL_SECONDS must be positive when resolver is enabled")
	}
	if cfg.BatchSize <= 0 {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_BATCH_SIZE must be positive when resolver is enabled")
	}
	if cfg.MaxResolvePerRun <= 0 {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RESOLVE_PER_RUN must be positive when resolver is enabled")
	}
	if cfg.MaxRunDuration <= 0 {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS must be positive when resolver is enabled")
	}
	if cfg.ResolveTimeout <= 0 {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_RESOLVE_TIMEOUT_SECONDS must be positive when resolver is enabled")
	}
	if cfg.MaxRunDuration < cfg.ResolveTimeout {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS must be >= SCRAPER_PUBLISHED_AT_RESOLVER_RESOLVE_TIMEOUT_SECONDS")
	}
	if cfg.MinDetectedAge <= 0 {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_MIN_DETECTED_AGE_SECONDS must be positive when resolver is enabled")
	}
	if cfg.FailureBackoffTTL <= 0 {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_FAILURE_BACKOFF_SECONDS must be positive when resolver is enabled")
	}
	return nil
}

func loadScraperPoll() ScraperPoll {
	defaults := DefaultScraperPoll()

	return ScraperPoll{
		Videos: secondsAliasEnv([]string{
			"SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS",
			"SCRAPER_VIDEOS_SECONDS",
		}, defaults.Videos),
		Shorts: secondsAliasEnv([]string{
			"SCRAPER_POLL_SHORTS_INTERVAL_SECONDS",
			"SCRAPER_SHORTS_SECONDS",
		}, defaults.Shorts),
		Community: secondsAliasEnv([]string{
			"SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS",
			"SCRAPER_COMMUNITY_SECONDS",
		}, defaults.Community),
		Stats: secondsAliasEnv([]string{
			"SCRAPER_POLL_STATS_INTERVAL_SECONDS",
			"SCRAPER_STATS_SECONDS",
		}, defaults.Stats),
		Live: secondsAliasEnv([]string{
			"SCRAPER_POLL_LIVE_INTERVAL_SECONDS",
			"SCRAPER_LIVE_SECONDS",
		}, defaults.Live),
	}
}

func secondsAliasEnv(keys []string, fallback time.Duration) time.Duration {
	for _, key := range keys {
		seconds := sharedenv.Int(key, 0)
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}

func intAliasEnv(keys []string, fallback int) int {
	for _, key := range keys {
		value := sharedenv.Int(key, 0)
		if value > 0 {
			return value
		}
	}
	return fallback
}

func isPlaceholderAPIKey(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "your_api_key", "your_youtube_api_key", "changeme", "change_me", "replace_me", "replace-with-real-key":
		return true
	default:
		return false
	}
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
