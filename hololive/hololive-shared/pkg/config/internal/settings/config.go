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

package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/park285/iris-client-go/iris"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
	"github.com/park285/shared-go/pkg/workerconfig"
)

type Config struct {
	Iris                 IrisConfig
	Server               ServerConfig
	Kakao                KakaoConfig
	Holodex              HolodexConfig
	YouTube              YouTubeConfig
	Ingestion            IngestionConfig
	Chzzk                ChzzkConfig
	Twitch               TwitchConfig
	Valkey               ValkeyConfig
	Postgres             PostgresConfig
	Notification         NotificationConfig
	Logging              LoggingConfig
	Bot                  BotConfig
	Services             ServicesConfig
	Environment          string
	Scraper              ScraperConfig
	Webhook              WebhookConfig
	WorkerPool           WorkerPoolConfig
	WorkerProfile        WorkerProfileConfig
	CORS                 CORSConfig
	Cliproxy             CliproxyConfig
	LLM                  LLMConfig
	Exa                  ExaConfig
	OfficialSchedule     OfficialScheduleConfig
	OfficialProfile      OfficialProfileConfig
	MaxResponseBodyBytes int64
	LLMSchedulerURL      string
	Version              string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction := loadRuntimeTokensAndCORS()
	config, err := buildConfig(webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction)
	if err != nil {
		return nil, err
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

//nolint:funlen // central environment-to-config assembly is intentionally kept in one place
func buildConfig(webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) (*Config, error) {
	communityShortsBigBangCutoverAt, err := loadCommunityShortsBigBangCutoverAt()
	if err != nil {
		return nil, err
	}
	irisConfig := loadIrisConfig(webhookToken, botToken)
	workerProfile, err := fetchIrisBotWebhookWorkerProfile(irisConfig)
	if err != nil && !errors.Is(err, workerconfig.ErrWorkerProfileDisabled) {
		return nil, fmt.Errorf("fetch Iris bot webhook worker profile: %w", err)
	}

	return &Config{
		Iris:    irisConfig,
		Server:  loadServerConfig(),
		Kakao:   loadKakaoConfig(),
		Holodex: loadHolodexConfig(),
		YouTube: loadYouTubeConfig(),
		Ingestion: IngestionConfig{
			YouTubeEnabled:                  sharedenv.Bool("YOUTUBE_INGESTION_ENABLED", true),
			PhotoSyncEnabled:                sharedenv.Bool("PHOTO_SYNC_ENABLED", true),
			CommunityShortsBigBangEnabled:   sharedenv.Bool("YOUTUBE_COMMUNITY_SHORTS_BIGBANG_ENABLED", false),
			CommunityShortsBigBangCutoverAt: communityShortsBigBangCutoverAt,
		},
		Valkey:       loadValkeyConfig(),
		Postgres:     loadPostgresConfig(),
		Notification: loadNotificationConfig(),
		Logging:      loadLoggingConfig(),
		Bot:          loadBotConfig(),
		Services:     loadServicesConfig(),
		Environment:  loadAppEnvironment(),
		Scraper:      loadScraperConfig(),
		Webhook:      loadWebhookConfig(workerProfile),
		WorkerPool:   loadWorkerPoolConfig(workerProfile),
		WorkerProfile: WorkerProfileConfig{
			Version: workerProfile.Version,
			Hash:    workerProfile.ProfileHash(),
		},
		Chzzk:                loadChzzkConfig(),
		Twitch:               loadTwitchConfig(),
		Cliproxy:             loadCliproxyConfig(),
		LLM:                  loadLLMConfig(),
		Exa:                  loadExaConfig(),
		OfficialSchedule:     loadOfficialScheduleConfig(),
		OfficialProfile:      loadOfficialProfileConfig(),
		MaxResponseBodyBytes: int64(sharedenv.Int("MAX_RESPONSE_BODY_BYTES", int(DefaultMaxResponseBodyBytes))),
		LLMSchedulerURL:      sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		CORS: CORSConfig{
			AllowedOrigins:      corsAllowedOrigins,
			Enforce:             sharedenv.Bool("CORS_ENFORCE", false),
			MissingInProduction: corsMissingInProduction,
		},
		Version: sharedenv.String("APP_VERSION", "1.1.0-go"),
	}, nil
}

func loadServicesConfig() ServicesConfig {
	return ServicesConfig{
		LLMSchedulerHealthURL: sharedenv.StringAny(
			"SERVICES_LLM_SCHEDULER_HEALTH_URL",
			"SERVICES_LLM_SERVER_HEALTH_URL",
		),
		GameBotTwentyQHealthURL: sharedenv.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
		GameBotTurtleHealthURL:  sharedenv.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
	}
}

func loadIrisConfig(webhookToken, botToken string) IrisConfig {
	return IrisConfig{
		BaseURL:                   sharedenv.String("IRIS_BASE_URL", ""),
		BaseURLFile:               sharedenv.String("IRIS_BASE_URL_FILE", ""),
		WebhookToken:              webhookToken,
		BotToken:                  botToken,
		HTTPTimeout:               time.Duration(sharedenv.Int("IRIS_HTTP_TIMEOUT_SECONDS", 10)) * time.Second,
		HTTPDialTimeout:           time.Duration(sharedenv.Int("IRIS_HTTP_DIAL_TIMEOUT_SECONDS", 3)) * time.Second,
		HTTPResponseHeaderTimeout: time.Duration(sharedenv.Int("IRIS_HTTP_RESP_HEADER_TIMEOUT_SECONDS", 5)) * time.Second,
	}
}

func fetchIrisBotWebhookWorkerProfile(config IrisConfig) (workerconfig.IrisBotWebhookWorkerProfile, error) {
	if strings.TrimSpace(config.BotToken) == "" {
		return workerconfig.LegacyIrisBotWebhookWorkerProfile(), workerconfig.ErrWorkerProfileDisabled
	}
	baseURL, err := resolveIrisBaseURL(config)
	if err != nil {
		return workerconfig.IrisBotWebhookWorkerProfile{}, err
	}
	irisClient, err := iris.NewClient(
		iris.WithBaseURL(baseURL),
		iris.WithBotToken(config.BotToken),
		iris.WithTransport(sharedenv.String("IRIS_TRANSPORT", "")),
		iris.WithTimeout(config.HTTPTimeout),
		iris.WithDialTimeout(config.HTTPDialTimeout),
		iris.WithResponseHeaderTimeout(config.HTTPResponseHeaderTimeout),
	)
	if err != nil {
		return workerconfig.IrisBotWebhookWorkerProfile{}, err
	}
	defer irisClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), config.HTTPTimeout)
	defer cancel()

	diagnostics, err := irisClient.GetRuntimeDiagnostics(ctx)
	if err != nil {
		return workerconfig.IrisBotWebhookWorkerProfile{}, err
	}
	return workerconfig.DecodeIrisBotWebhookWorkerProfileFromRuntimeDiagnostics(bytes.NewReader(diagnostics))
}

func resolveIrisBaseURL(config IrisConfig) (string, error) {
	if baseURL := strings.TrimSpace(config.BaseURL); baseURL != "" {
		return baseURL, nil
	}
	if path := strings.TrimSpace(config.BaseURLFile); path != "" {
		raw, err := os.ReadFile(path) //nolint:gosec // Runtime operator-provided Iris base URL file.
		if err != nil {
			return "", fmt.Errorf("read IRIS_BASE_URL_FILE: %w", err)
		}
		if baseURL := strings.TrimSpace(string(raw)); baseURL != "" {
			return baseURL, nil
		}
		return "", fmt.Errorf("IRIS_BASE_URL_FILE is empty")
	}
	return "", fmt.Errorf("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
}

func loadKakaoConfig() KakaoConfig {
	return KakaoConfig{
		Rooms:      parseCommaSeparated(sharedenv.String("KAKAO_ROOMS", "홀로라이브 알림방")),
		ACLEnabled: sharedenv.Bool("KAKAO_ACL_ENABLED", true),
		ACLMode:    sharedenv.String("KAKAO_ACL_MODE", "whitelist"),
	}
}

func loadLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:      sharedenv.String("LOG_LEVEL", "info"),
		Dir:        sharedenv.String("LOG_DIR", ""),
		MaxSizeMB:  sharedenv.Int("LOG_MAX_SIZE_MB", 5),
		MaxBackups: sharedenv.Int("LOG_MAX_BACKUPS", 5),
		MaxAgeDays: sharedenv.Int("LOG_MAX_AGE_DAYS", 30),
		Compress:   sharedenv.Bool("LOG_COMPRESS", true),
	}
}

func loadBotConfig() BotConfig {
	return BotConfig{
		Prefix:        sharedenv.String("BOT_PREFIX", "!"),
		SelfUser:      sharedenv.String("BOT_SELF_USER", "iris"),
		MentionPrefix: sharedenv.String("BOT_MENTION_PREFIX", "#kapu봇"),
	}
}

func loadHolodexConfig() HolodexConfig {
	d := DefaultHolodexOperationalConfig()
	return HolodexConfig{
		BaseURL:           sharedenv.String("HOLODEX_BASE_URL", d.BaseURL),
		APIKey:            resolveHolodexAPIKey(),
		Timeout:           time.Duration(sharedenv.Int("HOLODEX_TIMEOUT_SECONDS", int(d.Timeout/time.Second))) * time.Second,
		PerAttemptTimeout: time.Duration(sharedenv.Int("HOLODEX_PER_ATTEMPT_TIMEOUT_SECONDS", int(d.PerAttemptTimeout/time.Second))) * time.Second,
		MaxRetryAttempts:  sharedenv.Int("HOLODEX_MAX_RETRY_ATTEMPTS", d.MaxRetryAttempts),
		Transport: HolodexTransportConfig{
			MaxConnsPerHost:     sharedenv.Int("HOLODEX_MAX_CONNS_PER_HOST", d.Transport.MaxConnsPerHost),
			MaxIdleConnsPerHost: sharedenv.Int("HOLODEX_MAX_IDLE_CONNS_PER_HOST", d.Transport.MaxIdleConnsPerHost),
			IdleConnTimeout:     time.Duration(sharedenv.Int("HOLODEX_IDLE_CONN_TIMEOUT_SECONDS", int(d.Transport.IdleConnTimeout/time.Second))) * time.Second,
		},
		Concurrency: HolodexConcurrencyConfig{
			MaxConcurrentRequests: sharedenv.Int("HOLODEX_MAX_CONCURRENT_REQUESTS", d.Concurrency.MaxConcurrentRequests),
			OrgAllParallelism:     sharedenv.Int("HOLODEX_ORG_ALL_PARALLELISM", d.Concurrency.OrgAllParallelism),
			RequestDelay:          time.Duration(sharedenv.Int("HOLODEX_REQUEST_DELAY_MS", int(d.Concurrency.RequestDelay/time.Millisecond))) * time.Millisecond,
		},
		DistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    sharedenv.Bool("HOLODEX_DISTRIBUTED_RATELIMIT_ENABLED", d.DistributedRateLimit.Enabled),
			Limit:      sharedenv.Int("HOLODEX_DISTRIBUTED_RATELIMIT_LIMIT", d.DistributedRateLimit.Limit),
			Window:     time.Duration(sharedenv.Int("HOLODEX_DISTRIBUTED_RATELIMIT_WINDOW_MS", int(d.DistributedRateLimit.Window/time.Millisecond))) * time.Millisecond,
			KeyPrefix:  sharedenv.String("HOLODEX_DISTRIBUTED_RATELIMIT_KEY_PREFIX", d.DistributedRateLimit.KeyPrefix),
			BucketBase: sharedenv.String("HOLODEX_DISTRIBUTED_RATELIMIT_BUCKET_BASE", d.DistributedRateLimit.BucketBase),
		},
	}
}

func loadYouTubeConfig() YouTubeConfig {
	d := DefaultYouTubeOperationalConfig()
	producerInterval := time.Duration(sharedenv.Int("YOUTUBE_PRODUCER_REQUEST_INTERVAL_SECONDS", int(d.ProducerRequestInterval/time.Second))) * time.Second
	return YouTubeConfig{
		APIKey:                  sharedenv.String("YOUTUBE_API_KEY", ""),
		EnableQuotaBuilding:     sharedenv.Bool("YOUTUBE_ENABLE_QUOTA_BUILDING", false),
		DailyQuotaLimit:         sharedenv.Int("YOUTUBE_DAILY_QUOTA_LIMIT", d.DailyQuotaLimit),
		SearchQuotaCost:         sharedenv.Int("YOUTUBE_SEARCH_QUOTA_COST", d.SearchQuotaCost),
		ChannelsQuotaCost:       sharedenv.Int("YOUTUBE_CHANNELS_QUOTA_COST", d.ChannelsQuotaCost),
		MaxChannelsPerCall:      sharedenv.Int("YOUTUBE_MAX_CHANNELS_PER_CALL", d.MaxChannelsPerCall),
		MaxConcurrentRequests:   sharedenv.Int("YOUTUBE_MAX_CONCURRENT_REQUESTS", d.MaxConcurrentRequests),
		SearchMaxResults:        sharedenv.Int("YOUTUBE_SEARCH_MAX_RESULTS", d.SearchMaxResults),
		QuotaSafetyMargin:       sharedenv.Int("YOUTUBE_QUOTA_SAFETY_MARGIN", d.QuotaSafetyMargin),
		CacheExpiration:         time.Duration(sharedenv.Int("YOUTUBE_CACHE_EXPIRATION_SECONDS", int(d.CacheExpiration/time.Second))) * time.Second,
		MaxPageBodyBytes:        int64(sharedenv.Int("YOUTUBE_MAX_PAGE_BODY_BYTES", int(d.MaxPageBodyBytes))),
		ScraperHTTPTimeout:      time.Duration(sharedenv.Int("YOUTUBE_SCRAPER_HTTP_TIMEOUT_SECONDS", int(d.ScraperHTTPTimeout/time.Second))) * time.Second,
		ScraperDialTimeout:      time.Duration(sharedenv.Int("YOUTUBE_SCRAPER_DIAL_TIMEOUT_SECONDS", int(d.ScraperDialTimeout/time.Second))) * time.Second,
		ScraperHeaderTimeout:    time.Duration(sharedenv.Int("YOUTUBE_SCRAPER_HEADER_TIMEOUT_SECONDS", int(d.ScraperHeaderTimeout/time.Second))) * time.Second,
		ScraperPhaseTimeout:     time.Duration(sharedenv.Int("YOUTUBE_SCRAPER_PHASE_TIMEOUT_SECONDS", int(d.ScraperPhaseTimeout/time.Second))) * time.Second,
		APIFallbackTimeout:      time.Duration(sharedenv.Int("YOUTUBE_API_FALLBACK_TIMEOUT_SECONDS", int(d.APIFallbackTimeout/time.Second))) * time.Second,
		CacheSaveTimeout:        time.Duration(sharedenv.Int("YOUTUBE_CACHE_SAVE_TIMEOUT_SECONDS", int(d.CacheSaveTimeout/time.Second))) * time.Second,
		CommunityMissingTTL:     time.Duration(sharedenv.Int("YOUTUBE_COMMUNITY_MISSING_TTL_SECONDS", int(d.CommunityMissingTTL/time.Second))) * time.Second,
		VideoRSSBackoffTTL:      time.Duration(sharedenv.Int("YOUTUBE_VIDEO_RSS_BACKOFF_TTL_SECONDS", int(d.VideoRSSBackoffTTL/time.Second))) * time.Second,
		ProducerRequestInterval: producerInterval,
		ProducerDistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    sharedenv.Bool("YOUTUBE_PRODUCER_DISTRIBUTED_RATELIMIT_ENABLED", d.ProducerDistributedRateLimit.Enabled),
			Limit:      sharedenv.Int("YOUTUBE_PRODUCER_DISTRIBUTED_RATELIMIT_LIMIT", d.ProducerDistributedRateLimit.Limit),
			Window:     producerInterval,
			KeyPrefix:  sharedenv.String("YOUTUBE_PRODUCER_DISTRIBUTED_RATELIMIT_KEY_PREFIX", d.ProducerDistributedRateLimit.KeyPrefix),
			BucketBase: sharedenv.String("YOUTUBE_PRODUCER_DISTRIBUTED_RATELIMIT_BUCKET_BASE", d.ProducerDistributedRateLimit.BucketBase),
		},
	}
}

func loadChzzkConfig() ChzzkConfig {
	d := DefaultChzzkOperationalConfig()
	return ChzzkConfig{
		ClientID:                  sharedenv.String("CHZZK_CLIENT_ID", ""),
		ClientSecret:              sharedenv.String("CHZZK_CLIENT_SECRET", ""),
		MaxLivesPageSize:          sharedenv.Int("CHZZK_MAX_LIVES_PAGE_SIZE", d.MaxLivesPageSize),
		BatchLookupThreshold:      sharedenv.Int("CHZZK_BATCH_LOOKUP_THRESHOLD", d.BatchLookupThreshold),
		MaxConcurrentStatusChecks: sharedenv.Int("CHZZK_MAX_CONCURRENT_STATUS_CHECKS", d.MaxConcurrentStatusChecks),
	}
}

func loadTwitchConfig() TwitchConfig {
	d := DefaultTwitchOperationalConfig()
	return TwitchConfig{
		ClientID:           sharedenv.String("TWITCH_CLIENT_ID", ""),
		ClientSecret:       sharedenv.String("TWITCH_CLIENT_SECRET", ""),
		BaseURL:            sharedenv.String("TWITCH_BASE_URL", d.BaseURL),
		AuthURL:            sharedenv.String("TWITCH_AUTH_URL", d.AuthURL),
		Timeout:            time.Duration(sharedenv.Int("TWITCH_TIMEOUT_SECONDS", int(d.Timeout/time.Second))) * time.Second,
		PollInterval:       time.Duration(sharedenv.Int("TWITCH_POLL_INTERVAL_SECONDS", int(d.PollInterval/time.Second))) * time.Second,
		TokenRefreshSkew:   time.Duration(sharedenv.Int("TWITCH_TOKEN_REFRESH_SKEW_SECONDS", int(d.TokenRefreshSkew/time.Second))) * time.Second,
		MarkerTTL:          time.Duration(sharedenv.Int("TWITCH_MARKER_TTL_HOURS", int(d.MarkerTTL/time.Hour))) * time.Hour,
		MaxUsersPerRequest: sharedenv.Int("TWITCH_MAX_USERS_PER_REQUEST", d.MaxUsersPerRequest),
	}
}

func loadOfficialScheduleConfig() OfficialScheduleConfig {
	d := DefaultOfficialScheduleConfig()
	return OfficialScheduleConfig{
		BaseURL:      sharedenv.String("OFFICIAL_SCHEDULE_BASE_URL", d.BaseURL),
		Timeout:      time.Duration(sharedenv.Int("OFFICIAL_SCHEDULE_TIMEOUT_SECONDS", int(d.Timeout/time.Second))) * time.Second,
		CacheExpiry:  time.Duration(sharedenv.Int("OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS", int(d.CacheExpiry/time.Second))) * time.Second,
		PageCacheTTL: time.Duration(sharedenv.Int("OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS", int(d.PageCacheTTL/time.Second))) * time.Second,
	}
}

func loadOfficialProfileConfig() OfficialProfileConfig {
	d := DefaultOfficialProfileConfig()
	return OfficialProfileConfig{
		BaseURL:        sharedenv.String("OFFICIAL_PROFILE_BASE_URL", d.BaseURL),
		UserAgent:      sharedenv.String("OFFICIAL_PROFILE_USER_AGENT", d.UserAgent),
		AcceptLanguage: sharedenv.String("OFFICIAL_PROFILE_ACCEPT_LANGUAGE", d.AcceptLanguage),
		RequestTimeout: time.Duration(sharedenv.Int("OFFICIAL_PROFILE_REQUEST_TIMEOUT_SECONDS", int(d.RequestTimeout/time.Second))) * time.Second,
		DelayBetween:   time.Duration(sharedenv.Int("OFFICIAL_PROFILE_DELAY_BETWEEN_MS", int(d.DelayBetween/time.Millisecond))) * time.Millisecond,
		OutputFile:     sharedenv.String("OFFICIAL_PROFILE_OUTPUT_FILE", d.OutputFile),
	}
}
