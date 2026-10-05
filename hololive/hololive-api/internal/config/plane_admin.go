package config

import (
	"errors"
	"fmt"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// adminCORSDefaultEnforce: 관리 화면은 브라우저 표면이므로 CORS_ENFORCE가 없으면 허용 origin 검사를 강제한다.
const adminCORSDefaultEnforce = true

// AdminPlaneConfig는 admin plane(관리 HTTP API·room ACL 관리·운영 상태 조회)이 소비하는 설정만 담는다.
// Admin plane은 compose 보안 계약상 nonEgress라 Iris egress 입력을 갖지 않는다.
type AdminPlaneConfig struct {
	Environment      string
	Server           settings.ServerConfig
	InternalH3       sharedh3.ClientOptions
	Kakao            settings.KakaoConfig
	Holodex          settings.HolodexConfig
	OfficialSchedule settings.OfficialScheduleRuntimeConfig
	Valkey           settings.ValkeyConfig
	Postgres         settings.PostgresConfig
	Notification     settings.NotificationConfig
	CORS             settings.CORSConfig
	Ingestion        settings.IngestionConfig
	Services         settings.ServicesConfig
	// SettingsFilePath: 관리 화면이 저장하는 persisted settings(JSON) 경로. SETTINGS_DIR(기본 data)/settings.json.
	SettingsFilePath string
	LLMSchedulerURL  string
	AlarmServiceURL  string
	BotInternalURL   string
}

func loadAdminPlaneConfig() (*AdminPlaneConfig, error) {
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
	cors, corsErr := settings.LoadCORSConfig(adminCORSDefaultEnforce)
	ingestion, ingestionErr := settings.LoadIngestionConfig()

	if err := errors.Join(
		serverErr, holodexErr, officialScheduleErr, valkeyErr, postgresErr, notificationErr, corsErr, ingestionErr,
	); err != nil {
		return nil, fmt.Errorf("load admin plane sections: %w", err)
	}

	config := &AdminPlaneConfig{
		Environment:      envload.AppEnvironment(),
		Server:           server,
		InternalH3:       settings.LoadInternalH3ClientOptions(),
		Holodex:          holodex,
		OfficialSchedule: officialSchedule,
		Valkey:           valkey,
		Postgres:         postgres,
		Notification:     notification,
		CORS:             cors,
		Ingestion:        ingestion,
		Services:         settings.LoadServicesConfig(),
		SettingsFilePath: settings.LoadSettingsFilePath(),
		LLMSchedulerURL:  sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		AlarmServiceURL:  sharedenv.String("ALARM_INTERNAL_URL", ""),
		BotInternalURL:   sharedenv.String("HOLOLIVE_BOT_INTERNAL_URL", ""),
	}
	// KakaoConfig는 잠금을 포함하므로 값 복사 대신 필드를 옮긴다.
	config.Kakao.Rooms = kakao.Rooms
	config.Kakao.ACLEnabled = kakao.ACLEnabled
	config.Kakao.ACLMode = kakao.ACLMode

	return config, nil
}

// Validate는 admin plane 역할 조건을 검사한다. NonEgress라 Iris 입력 필수 검증은 면제하지만 room ACL seed와
// Holodex API 키, production CORS 허용 origin을 요구하고, proactive notification egress 소유를 거절한다.
func (c *AdminPlaneConfig) Validate() error {
	if c == nil {
		return errors.New("admin plane config is required")
	}

	if err := settings.ValidateServerRuntime(c.Environment, &c.Server); err != nil {
		return fmt.Errorf("validate server runtime: %w", err)
	}

	if err := settings.ValidateKakaoRooms(c.Kakao.Rooms); err != nil {
		return fmt.Errorf("validate kakao rooms: %w", err)
	}

	if err := runtimepolicy.ValidateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	if err := validatePlaneDependencies(c.Environment, &c.Postgres, &c.Holodex, c.OfficialSchedule); err != nil {
		return err
	}

	if err := settings.ValidateCORSConfig(c.Environment, c.CORS); err != nil {
		return fmt.Errorf("validate CORS config: %w", err)
	}

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeAdminAPI, envload.TrimmedEnv(runtimepolicy.NotificationEgressRoleEnv), envload.TrimmedEnv(runtimepolicy.NotificationSchedulerRoleEnv)); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}
