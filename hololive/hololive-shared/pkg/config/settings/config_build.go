package settings

import (
	"fmt"

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
	workerProfile := resolveIrisBotWebhookWorkerProfile(&irisConfig, options)
	scraperConfig := loadScraperConfig()
	collectorConfig, err := loadYouTubeCollectorConfig()
	if err != nil {
		return nil, err
	}
	tracingInstanceID := scraperConfig.ActiveActive.InstanceID
	if options.TracingRuntime == tracingRuntimeYouTubeCollector {
		tracingInstanceID = collectorConfig.InstanceID
	}
	tracingConfig, err := loadTracingConfig(options.TracingRuntime, tracingInstanceID)
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
	config := newBaseConfig(corsAllowedOrigins, corsMissingInProduction, options)
	config.Iris = irisConfig
	config.Kakao = newKakaoConfig(kakaoConfig.Rooms, kakaoConfig.ACLEnabled, kakaoConfig.ACLMode)
	config.YouTube = youtubeConfig
	config.Ingestion = loadIngestionConfig(communityShortsBigBangCutoverAt)
	config.Tracing = tracingConfig
	config.Scraper = scraperConfig
	config.YouTubeCollector = collectorConfig
	config.Webhook = loadWebhookConfig(&workerProfile)
	config.WorkerPool = loadWorkerPoolConfig(&workerProfile)
	config.WorkerProfile = WorkerProfileConfig{Version: workerProfile.Version, Hash: workerProfile.ProfileHash()}
	return config, nil
}

func newBaseConfig(corsAllowedOrigins []string, corsMissingInProduction bool, options configLoadOptions) *Config {
	return &Config{
		Server:                 loadServerConfig(),
		Holodex:                loadHolodexConfig(),
		Valkey:                 loadValkeyConfig(),
		Postgres:               loadPostgresConfig(),
		Notification:           loadNotificationConfig(),
		AlarmDispatchRetention: loadAlarmDispatchRetentionConfig(),
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
	}
}
