package config

import (
	"errors"
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// BotPlaneConfig는 bot plane(Iris webhook 수신·명령 응답·durable inbox/outbox)이 소비하는 설정만 담는다.
// 공통 env 형식 검사는 settings.ValidateRuntimeEnvSyntax가, 역할 검증은 Validate가 소유한다.
type BotPlaneConfig struct {
	Environment      string
	Server           settings.ServerConfig
	InternalH3       sharedh3.ClientOptions
	Iris             settings.IrisConfig
	Kakao            settings.KakaoConfig
	Holodex          settings.HolodexConfig
	OfficialSchedule settings.OfficialScheduleRuntimeConfig
	Valkey           settings.ValkeyConfig
	Postgres         settings.PostgresConfig
	Notification     settings.NotificationConfig
	Bot              settings.BotConfig
	Webhook          WebhookConfig
	APIWorkerProfile *APIWorkerProfile
	// SettingsFilePath: 관리 화면이 저장하는 persisted settings(JSON) 경로. SETTINGS_DIR(기본 data)/settings.json.
	SettingsFilePath string
	LLMSchedulerURL  string
	AlarmServiceURL  string
}

// WebhookConfig는 bot webhook 수신 한도다. 값은 Stack Worker Profile의 bot_webhook_inbox에서만 온다.
type WebhookConfig struct {
	MaxBodyBytes int64
	DedupTTL     time.Duration
	DedupTimeout time.Duration
}

// LoadBotPlaneRuntime은 bot plane 설정만 단독으로 읽고 검증한다. 전체 hololive-api 기동은 LoadRuntime을 쓰고,
// 이 함수는 bot plane DB 설정만 필요한 운영 명령이 같은 env 계약(퇴역 키·형식·tracing·역할 검증)을 거치게 한다.
func LoadBotPlaneRuntime() (*BotPlaneConfig, error) {
	if err := loadProcessEnv(); err != nil {
		return nil, err
	}

	tracing, err := settings.LoadTracingConfig(envload.TracingHololiveAPIEnabledEnv)
	if err != nil {
		return nil, fmt.Errorf("load tracing config: %w", err)
	}

	if err = settings.ValidateTracingConfig(tracing); err != nil {
		return nil, fmt.Errorf("validate tracing config: %w", err)
	}

	// 예전 bot 로더는 CORS_ENFORCE 기본값 false로 production 허용 origin 누락을 거절했다. 전체 기동에서는 admin plane의
	// 더 엄격한 검사가 같은 조건을 덮고, 단독 bot 로더는 같은 거절을 여기서 유지한다.
	cors, err := settings.LoadCORSConfig(false)
	if err != nil {
		return nil, fmt.Errorf("load CORS config: %w", err)
	}

	if err = settings.ValidateCORSConfig(envload.AppEnvironment(), cors); err != nil {
		return nil, fmt.Errorf("validate CORS config: %w", err)
	}

	config, err := loadBotPlaneConfig()
	if err != nil {
		return nil, fmt.Errorf("load bot plane config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("bot plane config validation failed: %w", err)
	}

	return config, nil
}

// loadProcessEnv는 .env를 읽고, bot·admin plane이 공유하는 퇴역 env 거절과 공통 env 형식 검사를 한 번 수행한다.
func loadProcessEnv() error {
	if err := envload.DotEnv(); err != nil {
		return fmt.Errorf("load dot env: %w", err)
	}

	if err := settings.RejectRetiredRuntimeEnv(); err != nil {
		return fmt.Errorf("reject retired runtime env: %w", err)
	}

	if err := settings.ValidateRuntimeEnvSyntax(); err != nil {
		return fmt.Errorf("validate runtime env syntax: %w", err)
	}

	return nil
}

func loadBotPlaneConfig() (*BotPlaneConfig, error) {
	iris, err := settings.LoadIrisConfig()
	if err != nil {
		return nil, fmt.Errorf("load iris config: %w", err)
	}

	kakao, err := settings.LoadKakaoConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kakao config: %w", err)
	}

	server, serverErr := settings.LoadServerConfig()
	holodex, holodexErr := settings.LoadHolodexConfig()
	officialSchedule, officialScheduleErr := settings.LoadOfficialScheduleRuntimeConfig()
	valkey, valkeyErr := settings.LoadValkeyConfig()
	postgres, postgresErr := settings.LoadPostgresConfig()
	notification, notificationErr := settings.LoadNotificationConfig()
	bot, botErr := settings.LoadBotConfig()

	if err = errors.Join(serverErr, holodexErr, officialScheduleErr, valkeyErr, postgresErr, notificationErr, botErr); err != nil {
		return nil, fmt.Errorf("load bot plane sections: %w", err)
	}

	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		return nil, fmt.Errorf("load API worker profile: %w", err)
	}

	config := &BotPlaneConfig{
		Environment:      envload.AppEnvironment(),
		Server:           server,
		InternalH3:       settings.LoadInternalH3ClientOptions(),
		Iris:             iris,
		Holodex:          holodex,
		OfficialSchedule: officialSchedule,
		Valkey:           valkey,
		Postgres:         postgres,
		Notification:     notification,
		Bot:              bot,
		Webhook:          webhookConfigFromProfile(profile),
		APIWorkerProfile: profile,
		SettingsFilePath: settings.LoadSettingsFilePath(),
		LLMSchedulerURL:  sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		AlarmServiceURL:  sharedenv.String("ALARM_INTERNAL_URL", ""),
	}
	// KakaoConfig는 잠금을 포함하므로 값 복사 대신 필드를 옮긴다.
	config.Kakao.Rooms = kakao.Rooms
	config.Kakao.ACLEnabled = kakao.ACLEnabled
	config.Kakao.ACLMode = kakao.ACLMode

	return config, nil
}

func webhookConfigFromProfile(profile *APIWorkerProfile) WebhookConfig {
	return WebhookConfig{
		MaxBodyBytes: profile.BotWebhookInbox.MaxBodyBytes,
		DedupTTL:     time.Duration(profile.BotWebhookInbox.DedupTTLMS) * time.Millisecond,
		DedupTimeout: time.Duration(profile.BotWebhookInbox.DedupTimeoutMS) * time.Millisecond,
	}
}

// Validate는 bot plane 역할 조건을 검사한다. Bot plane은 Iris egress runtime이므로 room ACL seed, Iris 토큰·base URL,
// Holodex API 키를 모두 요구하고, proactive notification egress 소유를 거절한다.
func (c *BotPlaneConfig) Validate() error {
	if c == nil {
		return errors.New("bot plane config is required")
	}

	if err := settings.ValidateServerRuntime(c.Environment, &c.Server); err != nil {
		return fmt.Errorf("validate server runtime: %w", err)
	}

	if err := c.validateRequired(); err != nil {
		return fmt.Errorf("validate required: %w", err)
	}

	if err := validatePlaneDependencies(c.Environment, &c.Postgres, &c.Holodex, c.OfficialSchedule); err != nil {
		return err
	}

	if c.APIWorkerProfile == nil {
		return errors.New("bot runtime requires Stack Worker Profile v1")
	}

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeBot, envload.TrimmedEnv(runtimepolicy.NotificationEgressRoleEnv), envload.TrimmedEnv(runtimepolicy.NotificationSchedulerRoleEnv)); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}

func (c *BotPlaneConfig) validateRequired() error {
	if err := settings.ValidateKakaoRooms(c.Kakao.Rooms); err != nil {
		return fmt.Errorf("validate kakao rooms: %w", err)
	}

	if err := settings.ValidateIrisEgressInputs(&c.Iris); err != nil {
		return fmt.Errorf("validate iris egress inputs: %w", err)
	}

	if err := runtimepolicy.ValidateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	return nil
}

// validatePlaneDependencies는 bot·admin plane이 같이 쓰는 DB·Holodex·공식 일정 검증 순서를 고정한다.
func validatePlaneDependencies(
	environment string,
	postgres *settings.PostgresConfig,
	holodex *settings.HolodexConfig,
	officialSchedule settings.OfficialScheduleRuntimeConfig,
) error {
	if err := runtimepolicy.ValidatePostgresSSLMode(environment, postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := settings.ValidateHolodexConfig(holodex); err != nil {
		return fmt.Errorf("validate holodex config: %w", err)
	}

	if err := settings.ValidateOfficialScheduleRuntimeConfig(officialSchedule); err != nil {
		return fmt.Errorf("validate official schedule config: %w", err)
	}

	return nil
}
