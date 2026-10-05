package config

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// RuntimeConfig는 alarm-worker가 소비하는 설정만 담는다. 공통 env 형식·퇴역 키 거절은 shared settings 정책을 다시 쓰고,
// 역할 조립과 검증(worker profile, notification 소유권, dispatch retention)은 이 패키지가 소유한다.
type RuntimeConfig struct {
	Environment      string
	Server           settings.ServerConfig
	Iris             settings.IrisConfig
	Holodex          settings.HolodexConfig
	OfficialSchedule settings.OfficialScheduleRuntimeConfig
	Valkey           settings.ValkeyConfig
	Postgres         settings.PostgresConfig
	Notification     settings.NotificationConfig
	Logging          settings.LoggingConfig
	Tracing          settings.TracingConfig
	// SettingsFilePath: 관리 화면이 저장하는 persisted settings(JSON) 경로. SETTINGS_DIR(기본 data)/settings.json.
	SettingsFilePath string
	// MarkdownReplies: BOT_MARKDOWN_REPLIES. 예약 알림 본문도 bot 응답과 같은 Markdown 표시 정책을 쓴다.
	MarkdownReplies    bool
	AlarmWorkerProfile *AlarmWorkerProfile
	DispatchRetention  DispatchRetentionConfig
}

// alarmWorkerCORSDefaultEnforce: alarm-worker는 브라우저 표면이 없지만, 예전과 같이 CORS_ENFORCE가 명시된 production
// 설정의 허용 origin 누락은 기동 실패로 유지한다.
const alarmWorkerCORSDefaultEnforce = false

func LoadRuntime() (*RuntimeConfig, error) {
	if err := envload.DotEnv(); err != nil {
		return nil, fmt.Errorf("load dot env: %w", err)
	}

	if err := settings.RejectRetiredRuntimeEnv(); err != nil {
		return nil, fmt.Errorf("reject retired runtime env: %w", err)
	}

	config, err := loadRuntimeConfig()
	if err != nil {
		return nil, err
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
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

	if err = settings.ValidateRuntimeEnvSyntax(); err != nil {
		return nil, fmt.Errorf("validate runtime env syntax: %w", err)
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
	officialSchedule, officialScheduleErr := settings.LoadOfficialScheduleRuntimeConfig()
	valkey, valkeyErr := settings.LoadValkeyConfig()
	postgres, postgresErr := settings.LoadPostgresConfig()
	notification, notificationErr := settings.LoadNotificationConfig()
	logging, loggingErr := settings.LoadLoggingConfig()
	bot, botErr := settings.LoadBotConfig()
	cors, corsErr := settings.LoadCORSConfig(alarmWorkerCORSDefaultEnforce)

	if err := errors.Join(
		serverErr, holodexErr, officialScheduleErr, valkeyErr, postgresErr, notificationErr, loggingErr, botErr, corsErr,
	); err != nil {
		return nil, fmt.Errorf("load alarm-worker sections: %w", err)
	}

	environment := envload.AppEnvironment()
	if err := settings.ValidateCORSConfig(environment, cors); err != nil {
		return nil, fmt.Errorf("validate CORS config: %w", err)
	}

	return &RuntimeConfig{
		Environment:      environment,
		Server:           server,
		Holodex:          holodex,
		OfficialSchedule: officialSchedule,
		Valkey:           valkey,
		Postgres:         postgres,
		Notification:     notification,
		Logging:          logging,
		SettingsFilePath: settings.LoadSettingsFilePath(),
		MarkdownReplies:  bot.MarkdownReplies,
	}, nil
}

// loadRoleSections는 alarm-worker만 읽는 퇴역 notification egress 가드, worker profile, dispatch retention을 채운다.
func loadRoleSections(config *RuntimeConfig) error {
	if err := rejectRetiredNotificationEgressEnv(); err != nil {
		return fmt.Errorf("reject retired notification egress env: %w", err)
	}

	profile, err := LoadWorkerProfile()
	if err != nil {
		return fmt.Errorf("load alarm worker profile: %w", err)
	}

	retention, err := loadDispatchRetentionConfig()
	if err != nil {
		return fmt.Errorf("load alarm dispatch retention config: %w", err)
	}

	config.AlarmWorkerProfile = profile
	config.DispatchRetention = retention

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

	if err := settings.ValidateOfficialScheduleRuntimeConfig(c.OfficialSchedule); err != nil {
		return fmt.Errorf("validate official schedule config: %w", err)
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
