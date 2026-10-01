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
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

type Config struct {
	Iris         IrisConfig
	Server       ServerConfig
	Kakao        KakaoConfig
	Holodex      HolodexConfig
	YouTube      YouTubeConfig
	Ingestion    IngestionConfig
	Valkey       ValkeyConfig
	Postgres     PostgresConfig
	Notification NotificationConfig
	Logging      LoggingConfig
	Tracing      TracingConfig
	Bot          BotConfig
	Services     ServicesConfig
	Environment  string
	// SettingsFilePath: 관리 화면이 저장하는 persisted settings(JSON) 경로. SETTINGS_DIR(기본 data)/settings.json.
	SettingsFilePath     string
	Webhook              WebhookConfig
	WorkerPool           WorkerPoolConfig
	APIWorkerProfile     *APIWorkerProfile
	AlarmWorkerProfile   *AlarmWorkerProfile
	CORS                 CORSConfig
	Cliproxy             CliproxyConfig
	LLM                  LLMConfig
	Exa                  ExaConfig
	OfficialSchedule     OfficialScheduleConfig
	MaxResponseBodyBytes int64
	LLMSchedulerURL      string
	AlarmServiceURL      string
	BotInternalURL       string
	Version              string
}

// LoadOptions: plane 패키지가 core Config 로딩을 조립할 때 쓰는 hook이다.
// Section은 role 전용 구획(worker profile 등)을 채우며 nil이면 건너뛴다.
type LoadOptions struct {
	Section            func(*Config) error
	CORSDefaultEnforce bool
	TracingRuntime     TracingRuntime
}

func LoadAdminAPIRuntime() (*Config, error) {
	out, err := LoadConfig((*Config).ValidateAdminAPIRuntime, LoadOptions{
		CORSDefaultEnforce: true,
		TracingRuntime:     TracingRuntimeHololiveAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("load config validated: %w", err)
	}

	return out, nil
}

// LoadConfig: .env를 읽고 core Config를 만든 뒤 호출자가 준 검증을 적용한다.
func LoadConfig(validate func(*Config) error, options LoadOptions) (*Config, error) {
	if err := load.DotEnv(); err != nil {
		return nil, fmt.Errorf("load dot env: %w", err)
	}

	webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction := LoadRuntimeTokensAndCORS()

	config, err := buildConfig(webhookToken, botToken, corsAllowedOrigins, corsMissingInProduction, options)
	if err != nil {
		return nil, fmt.Errorf("build config: %w", err)
	}

	if err := validate(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

func newKakaoConfig(rooms []string, enabled bool, mode string) KakaoConfig {
	return KakaoConfig{Rooms: rooms, ACLEnabled: enabled, ACLMode: mode}
}

func loadIngestionConfig() (IngestionConfig, error) {
	photoSyncEnabled, err := sharedenv.BoolE("PHOTO_SYNC_ENABLED", true)
	if err != nil {
		return IngestionConfig{}, fmt.Errorf("load ingestion config: %w", err)
	}

	return IngestionConfig{PhotoSyncEnabled: photoSyncEnabled}, nil
}

func loadCORSConfig(
	corsAllowedOrigins []string,
	corsMissingInProduction bool,
	options LoadOptions,
) (CORSConfig, error) {
	enforce, err := sharedenv.BoolE("CORS_ENFORCE", options.CORSDefaultEnforce)
	if err != nil {
		return CORSConfig{}, fmt.Errorf("load CORS config: %w", err)
	}

	return CORSConfig{
		AllowedOrigins:      corsAllowedOrigins,
		Enforce:             enforce,
		MissingInProduction: corsMissingInProduction,
	}, nil
}

func loadServicesConfig() (ServicesConfig, error) {
	if err := rejectRetiredServicesEnv(); err != nil {
		return ServicesConfig{}, fmt.Errorf("reject retired services env: %w", err)
	}

	return ServicesConfig{
		LLMSchedulerHealthURL:   sharedenv.String("SERVICES_LLM_SCHEDULER_HEALTH_URL", ""),
		GameBotTwentyQHealthURL: sharedenv.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
		GameBotTurtleHealthURL:  sharedenv.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
	}, nil
}

func loadIrisConfig(webhookToken, botToken string) (IrisConfig, error) {
	var env load.StrictEnv

	config := IrisConfig{
		BaseURL:                   sharedenv.String("IRIS_BASE_URL", ""),
		BaseURLFile:               sharedenv.String("IRIS_BASE_URL_FILE", ""),
		WebhookToken:              webhookToken,
		BotToken:                  botToken,
		HTTPTimeout:               env.Seconds("IRIS_HTTP_TIMEOUT_SECONDS", 10*time.Second),
		HTTPDialTimeout:           env.Seconds("IRIS_HTTP_DIAL_TIMEOUT_SECONDS", 3*time.Second),
		HTTPResponseHeaderTimeout: env.Seconds("IRIS_HTTP_RESP_HEADER_TIMEOUT_SECONDS", 5*time.Second),
	}

	if err := env.Err(); err != nil {
		return IrisConfig{}, fmt.Errorf("load iris config: %w", err)
	}

	return config, nil
}

func loadKakaoConfig() (*KakaoConfig, error) {
	enabled, err := loadKakaoACLEnabled()
	if err != nil {
		return nil, fmt.Errorf("load kakao ACL enabled: %w", err)
	}

	mode, err := loadKakaoACLMode()
	if err != nil {
		return nil, fmt.Errorf("load kakao ACL mode: %w", err)
	}

	// KAKAO_ROOMS는 ACL 첫 초기화 seed이고 항목은 정확한 signed i64 chatID여야 한다(acl.NewACLService가 검증).
	// 예전 방 이름 기본값은 chatID 단일 식별에서 어떤 방과도 매칭되지 않는 whitelist 행이 되므로 두지 않고,
	// 값이 없으면 validate*RequiredConfig의 "KAKAO_ROOMS is required"로 기동을 거절한다
	// (DEC-20260926-stack-hololive-room-acl-and-console-contract).
	return &KakaoConfig{
		Rooms:      load.CommaSeparated(sharedenv.String("KAKAO_ROOMS", "")),
		ACLEnabled: enabled,
		ACLMode:    mode,
	}, nil
}

func loadKakaoACLEnabled() (bool, error) {
	const key = kakaoACLEnabledEnv

	raw, found := os.LookupEnv(key)

	if !found {
		return true, nil
	}

	if strings.TrimSpace(raw) == "" {
		return false, fmt.Errorf("%s must not be empty", key)
	}

	enabled, err := sharedenv.BoolE(key, true)
	if err != nil {
		return false, fmt.Errorf("read bool env: %w", err)
	}

	return enabled, nil
}

func loadKakaoACLMode() (string, error) {
	const key = kakaoACLModeEnv

	raw, found := os.LookupEnv(key)

	if !found {
		return "whitelist", nil
	}

	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "whitelist", "blacklist":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid %s: %q", key, raw)
	}
}

func LoadLoggingConfig() (LoggingConfig, error) {
	var env load.StrictEnv

	config := LoggingConfig{
		Level:      sharedenv.String("LOG_LEVEL", "info"),
		Dir:        sharedenv.String("LOG_DIR", ""),
		MaxSizeMB:  env.Int("LOG_MAX_SIZE_MB", 5),
		MaxBackups: env.Int("LOG_MAX_BACKUPS", 5),
		MaxAgeDays: env.Int("LOG_MAX_AGE_DAYS", 30),
		Compress:   env.Bool("LOG_COMPRESS", true),
	}

	if err := env.Err(); err != nil {
		return LoggingConfig{}, fmt.Errorf("load logging config: %w", err)
	}

	return config, nil
}

const (
	seeMoreFoldEnv     = "BOT_SEE_MORE_FOLD"
	seeMoreFoldDefault = true
)

// LoadSeeMoreFold는 bot·llm plane이 공유하는 '전체보기' 접기 스위치를 읽는다. 기본값은 접기이며, 다른 bool env처럼
// 잘못된 값은 기본값으로 바꾸지 않고 오류로 돌려준다(PLN-20260926-stack-audit-refactoring T10).
func LoadSeeMoreFold() (bool, error) {
	var env load.StrictEnv

	fold := env.Bool(seeMoreFoldEnv, seeMoreFoldDefault)
	if err := env.Err(); err != nil {
		return false, fmt.Errorf("load see-more fold: %w", err)
	}

	return fold, nil
}

func loadBotConfig() (BotConfig, error) {
	var env load.StrictEnv

	seeMoreFold, foldErr := LoadSeeMoreFold()

	config := BotConfig{
		Prefix:                sharedenv.String("BOT_PREFIX", "!"),
		SelfUser:              sharedenv.String("BOT_SELF_USER", "iris"),
		MentionPrefix:         sharedenv.String("BOT_MENTION_PREFIX", "#kapu봇"),
		CalendarImageCacheDir: sharedenv.String("BOT_CALENDAR_IMAGE_CACHE_DIR", "data/calendar-cache"),
		CalendarEntryCacheTTL: env.Seconds("BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS", 24*time.Hour),
		SeeMoreFold:           seeMoreFold,
		MarkdownReplies:       env.Bool("BOT_MARKDOWN_REPLIES", false),
	}

	if err := errors.Join(env.Err(), foldErr); err != nil {
		return BotConfig{}, fmt.Errorf("load bot config: %w", err)
	}

	return config, nil
}

func loadHolodexConfig() (HolodexConfig, error) {
	apiKey, err := load.HolodexAPIKey()
	if err != nil {
		return HolodexConfig{}, fmt.Errorf("load holodex config: %w", err)
	}

	d := DefaultHolodexOperationalConfig()

	var env load.StrictEnv

	config := HolodexConfig{
		BaseURL:           sharedenv.String("HOLODEX_BASE_URL", d.BaseURL),
		APIKey:            apiKey,
		Timeout:           env.Seconds("HOLODEX_TIMEOUT_SECONDS", d.Timeout),
		PerAttemptTimeout: env.Seconds("HOLODEX_PER_ATTEMPT_TIMEOUT_SECONDS", d.PerAttemptTimeout),
		MaxRetryAttempts:  env.Int("HOLODEX_MAX_RETRY_ATTEMPTS", d.MaxRetryAttempts),
		Transport: HolodexTransportConfig{
			MaxConnsPerHost:     env.Int("HOLODEX_MAX_CONNS_PER_HOST", d.Transport.MaxConnsPerHost),
			MaxIdleConnsPerHost: env.Int("HOLODEX_MAX_IDLE_CONNS_PER_HOST", d.Transport.MaxIdleConnsPerHost),
			IdleConnTimeout:     env.Seconds("HOLODEX_IDLE_CONN_TIMEOUT_SECONDS", d.Transport.IdleConnTimeout),
		},
		Concurrency: HolodexConcurrencyConfig{
			MaxConcurrentRequests: env.Int("HOLODEX_MAX_CONCURRENT_REQUESTS", d.Concurrency.MaxConcurrentRequests),
			OrgAllParallelism:     env.Int("HOLODEX_ORG_ALL_PARALLELISM", d.Concurrency.OrgAllParallelism),
			RequestDelay:          env.Millis("HOLODEX_REQUEST_DELAY_MS", d.Concurrency.RequestDelay),
		},
		DistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    env.Bool("HOLODEX_DISTRIBUTED_RATELIMIT_ENABLED", d.DistributedRateLimit.Enabled),
			Limit:      env.Int("HOLODEX_DISTRIBUTED_RATELIMIT_LIMIT", d.DistributedRateLimit.Limit),
			Window:     env.Millis("HOLODEX_DISTRIBUTED_RATELIMIT_WINDOW_MS", d.DistributedRateLimit.Window),
			KeyPrefix:  sharedenv.String("HOLODEX_DISTRIBUTED_RATELIMIT_KEY_PREFIX", d.DistributedRateLimit.KeyPrefix),
			BucketBase: sharedenv.String("HOLODEX_DISTRIBUTED_RATELIMIT_BUCKET_BASE", d.DistributedRateLimit.BucketBase),
		},
	}

	if err := env.Err(); err != nil {
		return HolodexConfig{}, fmt.Errorf("load holodex config: %w", err)
	}

	return config, nil
}

func loadYouTubeConfig() (YouTubeConfig, error) {
	if err := rejectRetiredYouTubeProducerEnv(); err != nil {
		return YouTubeConfig{}, fmt.Errorf("reject retired youtube producer env: %w", err)
	}

	if err := rejectRetiredYouTubeConfigEnv(); err != nil {
		return YouTubeConfig{}, fmt.Errorf("reject retired youtube config env: %w", err)
	}

	d := DefaultYouTubeOperationalConfig()

	var env load.StrictEnv

	interval := env.Seconds("YOUTUBE_REQUEST_INTERVAL_SECONDS", d.RequestInterval)
	config := YouTubeConfig{
		MaxPageBodyBytes:     env.Int64("YOUTUBE_MAX_PAGE_BODY_BYTES", d.MaxPageBodyBytes),
		ScraperHTTPTimeout:   env.Seconds("YOUTUBE_SCRAPER_HTTP_TIMEOUT_SECONDS", d.ScraperHTTPTimeout),
		ScraperDialTimeout:   env.Seconds("YOUTUBE_SCRAPER_DIAL_TIMEOUT_SECONDS", d.ScraperDialTimeout),
		ScraperHeaderTimeout: env.Seconds("YOUTUBE_SCRAPER_HEADER_TIMEOUT_SECONDS", d.ScraperHeaderTimeout),
		CommunityMissingTTL:  env.Seconds("YOUTUBE_COMMUNITY_MISSING_TTL_SECONDS", d.CommunityMissingTTL),
		RequestInterval:      interval,
		DistributedRateLimit: DistributedRateLimitConfig{
			Enabled:    env.Bool("YOUTUBE_DISTRIBUTED_RATELIMIT_ENABLED", d.DistributedRateLimit.Enabled),
			Limit:      env.Int("YOUTUBE_DISTRIBUTED_RATELIMIT_LIMIT", d.DistributedRateLimit.Limit),
			Window:     interval,
			KeyPrefix:  sharedenv.String("YOUTUBE_DISTRIBUTED_RATELIMIT_KEY_PREFIX", d.DistributedRateLimit.KeyPrefix),
			BucketBase: sharedenv.String("YOUTUBE_DISTRIBUTED_RATELIMIT_BUCKET_BASE", d.DistributedRateLimit.BucketBase),
		},
	}

	if err := env.Err(); err != nil {
		return YouTubeConfig{}, fmt.Errorf("load youtube config: %w", err)
	}

	return config, nil
}

func loadOfficialScheduleConfig() (OfficialScheduleConfig, error) {
	if err := rejectRetiredOfficialScheduleEnv(); err != nil {
		return OfficialScheduleConfig{}, fmt.Errorf("reject retired official schedule env: %w", err)
	}

	d := DefaultOfficialScheduleConfig()

	var env load.StrictEnv

	config := OfficialScheduleConfig{
		BaseURL:      sharedenv.String("OFFICIAL_SCHEDULE_BASE_URL", d.BaseURL),
		Timeout:      env.Seconds("OFFICIAL_SCHEDULE_TIMEOUT_SECONDS", d.Timeout),
		PageCacheTTL: env.Seconds("OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS", d.PageCacheTTL),
	}

	if err := env.Err(); err != nil {
		return OfficialScheduleConfig{}, fmt.Errorf("load official schedule config: %w", err)
	}

	return config, nil
}
