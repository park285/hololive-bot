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
	"time"

	"github.com/joho/godotenv"
	sharedenv "github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"

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

//nolint:funlen // central environment-to-config assembly is intentionally kept in one place
func buildConfig(webhookToken, botToken string, corsAllowedOrigins []string, corsMissingInProduction bool) (*Config, error) {
	llmSchedulerHealthURL := sharedenv.StringAny(
		"SERVICES_LLM_SCHEDULER_HEALTH_URL",
		"SERVICES_LLM_SERVER_HEALTH_URL",
	)
	communityShortsBigBangCutoverAt, err := loadCommunityShortsBigBangCutoverAt()
	if err != nil {
		return nil, err
	}

	return &Config{
		Iris: IrisConfig{
			BaseURL:                   sharedenv.String("IRIS_BASE_URL", ""),
			BaseURLFile:               sharedenv.String("IRIS_BASE_URL_FILE", ""),
			WebhookToken:              webhookToken,
			BotToken:                  botToken,
			HTTPTimeout:               time.Duration(sharedenv.Int("IRIS_HTTP_TIMEOUT_SECONDS", 10)) * time.Second,
			HTTPDialTimeout:           time.Duration(sharedenv.Int("IRIS_HTTP_DIAL_TIMEOUT_SECONDS", 3)) * time.Second,
			HTTPResponseHeaderTimeout: time.Duration(sharedenv.Int("IRIS_HTTP_RESP_HEADER_TIMEOUT_SECONDS", 5)) * time.Second,
		},
		Server: loadServerConfig(),
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
		Valkey:       loadValkeyConfig(),
		Postgres:     loadPostgresConfig(),
		Notification: loadNotificationConfig(),
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
			MentionPrefix: sharedenv.String("BOT_MENTION_PREFIX", "#kapu봇"),
		},
		Services: ServicesConfig{
			LLMSchedulerHealthURL:   llmSchedulerHealthURL,
			GameBotTwentyQHealthURL: sharedenv.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
			GameBotTurtleHealthURL:  sharedenv.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
		},
		Environment: loadAppEnvironment(),
		Scraper:     loadScraperConfig(),
		Webhook:     loadWebhookConfig(),
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
