package settings

import (
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

func buildConfig(
	webhookToken, botToken string,
	corsAllowedOrigins []string,
	corsMissingInProduction bool,
	options configLoadOptions,
) (*Config, error) {
	communityShortsBigBangCutoverAt, err := loadCommunityShortsBigBangCutoverAt()
	if err != nil {
		return nil, err
	}
	irisConfig := loadIrisConfig(webhookToken, botToken)
	scraperConfig, err := loadScraperConfig()
	if err != nil {
		return nil, fmt.Errorf("load scraper config: %w", err)
	}
	tracingConfig, err := loadTracingConfig(options.TracingRuntime, scraperConfig.ActiveActive.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("load tracing config: %w", err)
	}
	kakaoConfig, err := loadKakaoConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kakao config: %w", err)
	}
	youtubeConfig, err := loadYouTubeConfig()
	if err != nil {
		return nil, err
	}
	config, err := newBaseConfig(corsAllowedOrigins, corsMissingInProduction, options)
	if err != nil {
		return nil, err
	}
	config.Iris = irisConfig
	config.Kakao = newKakaoConfig(kakaoConfig.Rooms, kakaoConfig.ACLEnabled, kakaoConfig.ACLMode)
	config.YouTube = youtubeConfig
	config.Ingestion = loadIngestionConfig(communityShortsBigBangCutoverAt)
	config.Tracing = tracingConfig
	config.Scraper = scraperConfig
	config.Webhook = loadWebhookConfig()
	if err := loadRoleWorkerProfile(config, options.WorkerProfileRole); err != nil {
		return nil, err
	}
	return config, nil
}

func loadRoleWorkerProfile(config *Config, role string) error {
	switch role {
	case "":
		return nil
	case "api":
		return loadAPIWorkerProfile(config)
	case "alarm-worker":
		return loadAlarmWorkerProfile(config)
	default:
		return fmt.Errorf("unsupported worker profile role %q", role)
	}
}

func loadAPIWorkerProfile(config *Config) error {
	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		return err
	}
	config.APIWorkerProfile = profile
	applyAPIWorkerProfile(config, profile)
	return nil
}

func loadAlarmWorkerProfile(config *Config) error {
	profile, err := LoadAlarmWorkerProfile()
	if err != nil {
		return err
	}
	config.AlarmWorkerProfile = profile
	return nil
}

func applyAPIWorkerProfile(config *Config, profile *APIWorkerProfile) {
	workers := profile.Loaded.Profile.Workers
	inbox := workers["bot_webhook_inbox"]
	config.Webhook.WorkerCount = inbox.Executor.ConfiguredWorkers
	config.Webhook.HandlerTimeout = workerDuration(inbox.Executor.AttemptTimeout)
	config.Webhook.MaxBodyBytes = profile.BotWebhookInbox.MaxBodyBytes
	config.Webhook.DedupTTL = time.Duration(profile.BotWebhookInbox.DedupTTLMS) * time.Millisecond
	config.Webhook.DedupTimeout = time.Duration(profile.BotWebhookInbox.DedupTimeoutMS) * time.Millisecond
}

func newBaseConfig(corsAllowedOrigins []string, corsMissingInProduction bool, options configLoadOptions) (*Config, error) {
	alarmDispatchRetention, err := loadAlarmDispatchRetentionConfig()
	if err != nil {
		return nil, fmt.Errorf("load alarm dispatch retention config: %w", err)
	}
	return &Config{
		Server:                 loadServerConfig(),
		Holodex:                loadHolodexConfig(),
		Valkey:                 loadValkeyConfig(),
		Postgres:               loadPostgresConfig(),
		Notification:           loadNotificationConfig(),
		AlarmDispatchRetention: alarmDispatchRetention,
		Logging:                loadLoggingConfig(),
		Bot:                    loadBotConfig(),
		Services:               loadServicesConfig(),
		Environment:            loadAppEnvironment(),
		Chzzk:                  loadChzzkConfig(),
		Twitch:                 loadTwitchConfig(),
		Cliproxy:               loadCliproxyConfig(),
		LLM:                    loadLLMConfig(),
		Exa:                    loadExaConfig(),
		OfficialSchedule:       loadOfficialScheduleConfig(),
		OfficialProfile:        loadOfficialProfileConfig(),
		MaxResponseBodyBytes:   int64(sharedenv.Int("MAX_RESPONSE_BODY_BYTES", int(DefaultMaxResponseBodyBytes))),
		LLMSchedulerURL:        sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		AlarmServiceURL:        sharedenv.String("ALARM_INTERNAL_URL", ""),
		BotInternalURL:         sharedenv.String("HOLOLIVE_BOT_INTERNAL_URL", ""),
		CORS:                   loadCORSConfig(corsAllowedOrigins, corsMissingInProduction, options),
		Version:                sharedenv.String("APP_VERSION", "1.1.0-go"),
	}, nil
}
