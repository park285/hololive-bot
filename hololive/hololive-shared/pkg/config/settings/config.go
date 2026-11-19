package settings

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	sharedenv "github.com/park285/shared-go/core/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
)

func LoadIrisConfig() (IrisConfig, error) {
	webhookToken, botToken := LoadIrisTokens()

	config := IrisConfig{
		BaseURL:                 sharedenv.String("IRIS_BASE_URL", ""),
		BaseURLFile:             sharedenv.String("IRIS_BASE_URL_FILE", ""),
		WebhookToken:            webhookToken,
		BotToken:                botToken,
		H3CACertFile:            sharedenv.String("IRIS_H3_CA_CERT_FILE", ""),
		H3ServerName:            sharedenv.String("IRIS_H3_SERVER_NAME", ""),
		BaseURLAllowedHosts:     splitIrisAllowedHosts(os.Getenv("IRIS_BASE_URL_ALLOWED_HOSTS")),
		ValidateBaseURLFileStat: strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production"),
	}

	transport, err := parseIrisTransport(os.Getenv("IRIS_TRANSPORT"))
	if err != nil {
		return IrisConfig{}, fmt.Errorf("load iris config: %w", err)
	}

	config.Transport = transport

	return config, nil
}

func parseIrisTransport(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return iris.TransportH3, nil
	}

	transport, err := iris.ParseTransport(raw)
	if err != nil {
		return "", fmt.Errorf("IRIS_TRANSPORT: %w", err)
	}

	return transport, nil
}

func splitIrisAllowedHosts(raw string) []string {
	var hosts []string

	for host := range strings.SplitSeq(raw, ",") {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}

	return hosts
}

func LoadLoggingConfig() (LoggingConfig, error) {
	var env envload.StrictEnv

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

func LoadHolodexConfig() (HolodexConfig, error) {
	apiKey := envload.HolodexAPIKey()

	d := DefaultHolodexOperationalConfig()

	var env envload.StrictEnv

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

func LoadOfficialScheduleRuntimeConfig() (OfficialScheduleRuntimeConfig, error) {
	officialSchedule, officialScheduleErr := loadOfficialScheduleConfig()
	maxResponseBodyBytes, maxResponseBodyBytesErr := loadMaxResponseBodyBytes()

	if err := errors.Join(officialScheduleErr, maxResponseBodyBytesErr); err != nil {
		return OfficialScheduleRuntimeConfig{}, err
	}

	return OfficialScheduleRuntimeConfig{
		OfficialSchedule:     officialSchedule,
		MaxResponseBodyBytes: maxResponseBodyBytes,
	}, nil
}

func loadOfficialScheduleConfig() (OfficialScheduleConfig, error) {
	d := DefaultOfficialScheduleConfig()

	var env envload.StrictEnv

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

const (
	seeMoreFoldEnv     = "BOT_SEE_MORE_FOLD"
	seeMoreFoldDefault = true
)

func LoadSeeMoreFold() (bool, error) {
	var env envload.StrictEnv

	fold := env.Bool(seeMoreFoldEnv, seeMoreFoldDefault)
	if err := env.Err(); err != nil {
		return false, fmt.Errorf("load see-more fold: %w", err)
	}

	return fold, nil
}

func LoadBotConfig() (BotConfig, error) {
	var env envload.StrictEnv

	seeMoreFold, foldErr := LoadSeeMoreFold()

	config := BotConfig{
		Prefix:                sharedenv.String("BOT_PREFIX", "!"),
		SelfUser:              sharedenv.String("BOT_SELF_USER", "iris"),
		CalendarEntryCacheTTL: env.Seconds("BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS", 24*time.Hour),
		SeeMoreFold:           seeMoreFold,
		MarkdownReplies:       env.Bool("BOT_MARKDOWN_REPLIES", false),
	}

	if err := errors.Join(env.Err(), foldErr); err != nil {
		return BotConfig{}, fmt.Errorf("load bot config: %w", err)
	}

	return config, nil
}
