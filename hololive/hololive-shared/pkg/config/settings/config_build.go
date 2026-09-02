package settings

import (
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

func buildConfig(
	webhookToken, botToken string,
	corsAllowedOrigins []string,
	corsMissingInProduction bool,
	options LoadOptions,
) (*Config, error) {
	communityShortsBigBangCutoverAt, err := loadCommunityShortsBigBangCutoverAt()
	if err != nil {
		return nil, fmt.Errorf("load community shorts big bang cutover at: %w", err)
	}

	irisConfig := loadIrisConfig(webhookToken, botToken)

	scraperConfig, err := loadScraperConfig()
	if err != nil {
		return nil, fmt.Errorf("load scraper config: %w", err)
	}

	tracingConfig, err := LoadTracingConfig(options.TracingRuntime, scraperConfig.ActiveActive.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("load tracing config: %w", err)
	}

	kakaoConfig, err := loadKakaoConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kakao config: %w", err)
	}

	youtubeConfig, err := loadYouTubeConfig()
	if err != nil {
		return nil, fmt.Errorf("load youtube config: %w", err)
	}

	config := newBaseConfig(corsAllowedOrigins, corsMissingInProduction, options)

	config.Iris = irisConfig
	config.Kakao = newKakaoConfig(kakaoConfig.Rooms, kakaoConfig.ACLEnabled, kakaoConfig.ACLMode)
	config.YouTube = youtubeConfig
	config.Ingestion = loadIngestionConfig(communityShortsBigBangCutoverAt)
	config.Tracing = tracingConfig
	config.Scraper = scraperConfig
	config.Webhook = loadWebhookConfig()

	if options.Section != nil {
		if err := options.Section(config); err != nil {
			return nil, fmt.Errorf("load runtime section: %w", err)
		}
	}

	return config, nil
}

func loadAPIWorkerProfile(config *Config) error {
	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		return fmt.Errorf("load API worker profile: %w", err)
	}

	config.APIWorkerProfile = profile
	applyAPIWorkerProfile(config, profile)

	return nil
}

func applyAPIWorkerProfile(config *Config, profile *APIWorkerProfile) {
	workers := profile.Loaded.Profile.Workers
	inbox := workers["bot_webhook_inbox"]

	config.Webhook.WorkerCount = inbox.Executor.ConfiguredWorkers
	config.Webhook.HandlerTimeout = load.WorkerDuration(inbox.Executor.AttemptTimeout)
	config.Webhook.MaxBodyBytes = profile.BotWebhookInbox.MaxBodyBytes
	config.Webhook.DedupTTL = time.Duration(profile.BotWebhookInbox.DedupTTLMS) * time.Millisecond
	config.Webhook.DedupTimeout = time.Duration(profile.BotWebhookInbox.DedupTimeoutMS) * time.Millisecond
}

func newBaseConfig(corsAllowedOrigins []string, corsMissingInProduction bool, options LoadOptions) *Config {
	return &Config{
		Server:               loadServerConfig(),
		Holodex:              loadHolodexConfig(),
		Valkey:               LoadValkeyConfig(),
		Postgres:             LoadPostgresConfig(),
		Notification:         loadNotificationConfig(),
		Logging:              LoadLoggingConfig(),
		Bot:                  loadBotConfig(),
		Services:             loadServicesConfig(),
		Environment:          load.AppEnvironment(),
		SettingsFilePath:     loadSettingsFilePath(),
		Chzzk:                loadChzzkConfig(),
		Twitch:               loadTwitchConfig(),
		Cliproxy:             LoadCliproxyConfig(),
		LLM:                  LoadLLMConfig(),
		Exa:                  LoadExaConfig(),
		OfficialSchedule:     loadOfficialScheduleConfig(),
		OfficialProfile:      loadOfficialProfileConfig(),
		MaxResponseBodyBytes: int64(sharedenv.Int("MAX_RESPONSE_BODY_BYTES", int(DefaultMaxResponseBodyBytes))),
		LLMSchedulerURL:      sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		AlarmServiceURL:      sharedenv.String("ALARM_INTERNAL_URL", ""),
		BotInternalURL:       sharedenv.String("HOLOLIVE_BOT_INTERNAL_URL", ""),
		CORS:                 loadCORSConfig(corsAllowedOrigins, corsMissingInProduction, options),
		Version:              sharedenv.String("APP_VERSION", "1.1.0-go"),
	}
}
