package settings

import (
	"errors"
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

func buildConfig(
	webhookToken, botToken string,
	corsAllowedOrigins []string,
	corsMissingInProduction bool,
	options LoadOptions,
) (*Config, error) {
	if err := rejectRetiredRuntimeEnv(); err != nil {
		return nil, err
	}

	irisConfig, err := loadIrisConfig(webhookToken, botToken)
	if err != nil {
		return nil, fmt.Errorf("load iris config: %w", err)
	}

	tracingConfig, err := LoadTracingConfig(options.TracingEnabledEnv)
	if err != nil {
		return nil, fmt.Errorf("load tracing config: %w", err)
	}

	kakaoConfig, err := loadKakaoConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kakao config: %w", err)
	}

	config, err := newBaseConfig(corsAllowedOrigins, corsMissingInProduction, options)
	if err != nil {
		return nil, fmt.Errorf("load base config: %w", err)
	}

	config.Iris = irisConfig
	config.Kakao = newKakaoConfig(kakaoConfig.Rooms, kakaoConfig.ACLEnabled, kakaoConfig.ACLMode)
	config.Tracing = tracingConfig

	if options.Section != nil {
		if err := options.Section(config); err != nil {
			return nil, fmt.Errorf("load runtime section: %w", err)
		}
	}

	return config, nil
}

// rejectRetiredRuntimeEnv는 settings.LoadConfig runtime(hololive-api bot·admin plane, alarm-worker)이 읽기 전에 거절하는
// 퇴역 env 가드를 차례로 실행한다. 각 가드의 제거 조건과 재검토 기한은 해당 config_*_retired_env.go 주석이 소유한다.
func rejectRetiredRuntimeEnv() error {
	if err := RejectRetiredLLMEnv(); err != nil {
		return fmt.Errorf("reject retired LLM env: %w", err)
	}

	if err := rejectRetiredHolodexLiveStatusFallbackEnv(); err != nil {
		return fmt.Errorf("reject retired holodex live-status fallback env: %w", err)
	}

	if err := rejectRetiredScraperFetchEnv(); err != nil {
		return fmt.Errorf("reject retired scraper fetch env: %w", err)
	}

	if err := rejectRetiredScraperConfigEnv(); err != nil {
		return fmt.Errorf("reject retired scraper config env: %w", err)
	}

	if err := rejectRetiredIngestionEnv(); err != nil {
		return fmt.Errorf("reject retired ingestion env: %w", err)
	}

	if err := rejectRetiredIrisEnv(); err != nil {
		return fmt.Errorf("reject retired iris env: %w", err)
	}

	if err := rejectRetiredWebhookEnv(); err != nil {
		return fmt.Errorf("reject retired webhook env: %w", err)
	}

	if err := RejectRetiredOutboxV3HandoffEnv(); err != nil {
		return fmt.Errorf("reject retired outbox v3 handoff env: %w", err)
	}

	if err := rejectRetiredRateLimiterInstanceIDEnv(); err != nil {
		return fmt.Errorf("reject retired rate limiter instance id env: %w", err)
	}

	return nil
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
	config.Webhook.HandlerTimeout = runtimepolicy.WorkerDuration(inbox.Executor.AttemptTimeout)
	config.Webhook.MaxBodyBytes = profile.BotWebhookInbox.MaxBodyBytes
	config.Webhook.DedupTTL = time.Duration(profile.BotWebhookInbox.DedupTTLMS) * time.Millisecond
	config.Webhook.DedupTimeout = time.Duration(profile.BotWebhookInbox.DedupTimeoutMS) * time.Millisecond
}

// newBaseConfig는 공통 구획을 모두 읽은 뒤 오류를 합쳐 돌려준다. 잘못된 env가 이 공통 구획 여러 곳에 있어도
// 한 번의 기동 실패로 모두 보이도록 첫 오류에서 멈추지 않으며, 오류가 하나라도 있으면 만든 설정은 버린다.
// 이보다 먼저 buildConfig가 읽는 iris·tracing·kakao 로더는
// 첫 오류에서 반환하므로 여기에 합쳐지지 않는다.
func newBaseConfig(
	corsAllowedOrigins []string,
	corsMissingInProduction bool,
	options LoadOptions,
) (*Config, error) {
	server, serverErr := loadServerConfig()
	holodex, holodexErr := loadHolodexConfig()
	valkey, valkeyErr := LoadValkeyConfig()
	postgres, postgresErr := LoadPostgresConfig()
	notification, notificationErr := loadNotificationConfig()
	logging, loggingErr := LoadLoggingConfig()
	bot, botErr := loadBotConfig()
	services, servicesErr := loadServicesConfig()
	cliproxy, cliproxyErr := LoadCliproxyConfig()
	llm, llmErr := LoadLLMConfig()
	exa, exaErr := LoadExaConfig()
	officialSchedule, officialScheduleErr := loadOfficialScheduleConfig()
	maxResponseBodyBytes, maxResponseBodyBytesErr := loadMaxResponseBodyBytes()
	cors, corsErr := loadCORSConfig(corsAllowedOrigins, corsMissingInProduction, options)
	ingestion, ingestionErr := loadIngestionConfig()

	if err := errors.Join(
		serverErr, holodexErr, valkeyErr, postgresErr, notificationErr, loggingErr, botErr, servicesErr,
		cliproxyErr, llmErr, exaErr, officialScheduleErr, maxResponseBodyBytesErr, corsErr, ingestionErr,
	); err != nil {
		return nil, err
	}

	return &Config{
		InternalH3:           LoadInternalH3ClientOptions(),
		Server:               server,
		Holodex:              holodex,
		Valkey:               valkey,
		Postgres:             postgres,
		Notification:         notification,
		Logging:              logging,
		Bot:                  bot,
		Services:             services,
		Environment:          envload.AppEnvironment(),
		SettingsFilePath:     loadSettingsFilePath(),
		Cliproxy:             cliproxy,
		LLM:                  llm,
		Exa:                  exa,
		OfficialSchedule:     officialSchedule,
		MaxResponseBodyBytes: maxResponseBodyBytes,
		LLMSchedulerURL:      sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		AlarmServiceURL:      sharedenv.String("ALARM_INTERNAL_URL", ""),
		BotInternalURL:       sharedenv.String("HOLOLIVE_BOT_INTERNAL_URL", ""),
		CORS:                 cors,
		Ingestion:            ingestion,
		Version:              sharedenv.String("APP_VERSION", "1.1.0-go"),
	}, nil
}
