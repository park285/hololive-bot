package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// RuntimeConfig는 alarm-worker가 소비하는 설정만 담는다. 공통 env 형식 검증은 shared settings 정책을 다시 쓰고,
// 역할 조립과 검증(worker profile, notification 소유권, dispatch retention)은 이 패키지가 소유한다.
type RuntimeConfig struct {
	Environment  string
	Server       settings.ServerConfig
	Iris         settings.IrisConfig
	Holodex      settings.HolodexConfig
	Valkey       settings.ValkeyConfig
	Postgres     settings.PostgresConfig
	Notification settings.NotificationConfig
	Logging      settings.LoggingConfig
	Tracing      settings.TracingConfig
	// SettingsFilePath: 관리 화면이 저장하는 persisted settings(JSON) 경로. SETTINGS_DIR(기본 data)/settings.json.
	SettingsFilePath string
	// MarkdownReplies: BOT_MARKDOWN_REPLIES. 예약 알림 본문도 bot 응답과 같은 Markdown 표시 정책을 쓴다.
	MarkdownReplies    bool
	AlarmWorkerProfile *AlarmWorkerProfile
	DispatchRetention  DispatchRetentionConfig
	// AlarmDispatchPublish: ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH. wakeup 여부는 worker profile alarm_dispatch.wakeup_enabled가 소유한다.
	AlarmDispatchPublish AlarmDispatchPublishConfig
}

// AlarmDispatchPublishConfig는 알림 발행자가 outbox에 한 번에 넣는 delivery 상한이다.
type AlarmDispatchPublishConfig struct {
	MaxDeliveriesPerBatch int
}

func LoadRuntime() (*RuntimeConfig, error) {
	if err := envload.DotEnv(); err != nil {
		return nil, fmt.Errorf("load dot env: %w", err)
	}

	config, err := loadRuntimeConfig()
	if err != nil {
		return nil, err
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return config, nil
}

func loadRuntimeConfig() (*RuntimeConfig, error) {
	iris, err := settings.LoadIrisConfig()
	if err != nil {
		return nil, fmt.Errorf("load iris config: %w", err)
	}

	tracing, err := settings.LoadTracingConfig(envload.TracingAlarmWorkerEnabledEnv)
	if err != nil {
		return nil, fmt.Errorf("load tracing config: %w", err)
	}

	// alarm-worker는 room ACL을 소비하지 않지만 compose 계약상 Iris egress runtime이라 KAKAO_ROOMS·KAKAO_ACL_* 입력을
	// bot plane과 같은 기준으로 거절한다. 값은 보관하지 않는다.
	kakao, err := settings.LoadKakaoConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kakao config: %w", err)
	}

	config, err := loadRuntimeSections()
	if err != nil {
		return nil, err
	}

	config.Iris = iris
	config.Tracing = tracing

	if err := validateEgressInputs(kakao.Rooms, &config.Iris, config.Holodex.APIKey); err != nil {
		return nil, fmt.Errorf("validate required: %w", err)
	}

	if err := loadRoleSections(config); err != nil {
		return nil, err
	}

	return config, nil
}

// loadRuntimeSections는 alarm-worker가 보관하는 공통 구획을 읽고 오류를 모두 합쳐 돌려준다.
func loadRuntimeSections() (*RuntimeConfig, error) {
	server, serverErr := settings.LoadServerConfig()
	holodex, holodexErr := settings.LoadHolodexConfig()
	valkey, valkeyErr := settings.LoadValkeyConfig()
	postgres, postgresErr := settings.LoadPostgresConfig()
	notification, notificationErr := settings.LoadNotificationConfig()
	logging, loggingErr := settings.LoadLoggingConfig()
	bot, botErr := settings.LoadBotConfig()

	if err := errors.Join(
		serverErr, holodexErr, valkeyErr, postgresErr, notificationErr, loggingErr, botErr,
	); err != nil {
		return nil, fmt.Errorf("load alarm-worker sections: %w", err)
	}

	return &RuntimeConfig{
		Environment:      envload.AppEnvironment(),
		Server:           server,
		Holodex:          holodex,
		Valkey:           valkey,
		Postgres:         postgres,
		Notification:     notification,
		Logging:          logging,
		SettingsFilePath: settings.LoadSettingsFilePath(),
		MarkdownReplies:  bot.MarkdownReplies,
	}, nil
}

// loadRoleSections는 alarm-worker의 worker profile, dispatch retention, 발행 batch 한도를 채운다.
func loadRoleSections(config *RuntimeConfig) error {
	profile, err := LoadWorkerProfile()
	if err != nil {
		return fmt.Errorf("load alarm worker profile: %w", err)
	}

	retention, err := loadDispatchRetentionConfig()
	if err != nil {
		return fmt.Errorf("load alarm dispatch retention config: %w", err)
	}

	// 잘못된 batch 한도는 기본값으로 바꾸지 않고 기동 오류로 드러낸다.
	maxDeliveries, err := envload.RequiredPositiveIntEnv("ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH", 1000)
	if err != nil {
		return fmt.Errorf("load alarm dispatch publish config: %w", err)
	}

	config.AlarmWorkerProfile = profile
	config.DispatchRetention = retention
	config.AlarmDispatchPublish = AlarmDispatchPublishConfig{MaxDeliveriesPerBatch: maxDeliveries}

	return nil
}

func validateEgressInputs(rooms []string, iris *settings.IrisConfig, holodexAPIKey string) error {
	if err := settings.ValidateKakaoRooms(rooms); err != nil {
		return fmt.Errorf("validate kakao rooms: %w", err)
	}

	if err := settings.ValidateIrisEgressInputs(iris); err != nil {
		return fmt.Errorf("validate iris egress inputs: %w", err)
	}

	if err := runtimepolicy.ValidateHolodexAPIKey(holodexAPIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	return nil
}

func (c *RuntimeConfig) validate() error {
	if c.Postgres.PoolMaxConns < 2 {
		return errors.New("POSTGRES_POOL_MAX_CONNS must be at least 2 to reserve alarm control capacity")
	}

	if c.Postgres.PoolMinConns < 0 || c.Postgres.PoolMinConns > c.Postgres.PoolMaxConns {
		return errors.New("POSTGRES_POOL_MIN_CONNS must be nonnegative and not exceed POSTGRES_POOL_MAX_CONNS")
	}

	if strings.TrimSpace(c.Postgres.User) != runtimepolicy.PostgresRuntimeRoleUser {
		return fmt.Errorf("alarm-worker requires POSTGRES_USER=%s", runtimepolicy.PostgresRuntimeRoleUser)
	}

	if err := settings.ValidateServerRuntime(c.Environment, &c.Server); err != nil {
		return fmt.Errorf("validate server runtime: %w", err)
	}

	if err := runtimepolicy.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := settings.ValidateTracingConfig(c.Tracing); err != nil {
		return fmt.Errorf("validate tracing config: %w", err)
	}

	if err := settings.ValidateHolodexConfig(&c.Holodex); err != nil {
		return fmt.Errorf("validate holodex config: %w", err)
	}

	if c.AlarmWorkerProfile == nil {
		return errors.New("alarm-worker runtime requires Stack Worker Profile v1")
	}

	if err := validateOwnership(c.Environment); err != nil {
		return fmt.Errorf("validate alarm worker ownership: %w", err)
	}

	if !runtimepolicy.IsProduction(c.Environment) {
		return nil
	}

	if err := validateProductionExecutors(c.AlarmWorkerProfile); err != nil {
		return fmt.Errorf("validate production alarm executors: %w", err)
	}

	return nil
}
