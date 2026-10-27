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
// Holodex·공식 일정 설정은 process 하나의 Holodex 서비스를 만드는 bot plane 설정이 소유한다.
// Admin plane은 compose 보안 계약상 nonEgress라 Iris egress 입력을 갖지 않는다.
type AdminPlaneConfig struct {
	Environment  string
	Server       settings.ServerConfig
	InternalH3   sharedh3.ClientOptions
	Valkey       settings.ValkeyConfig
	Postgres     settings.PostgresConfig
	Notification settings.NotificationConfig
	CORS         settings.CORSConfig
	Ingestion    settings.IngestionConfig
	// SettingsFilePath: 관리 화면이 저장하는 persisted settings(JSON) 경로. SETTINGS_DIR(기본 data)/settings.json.
	SettingsFilePath string
	AlarmServiceURL  string
}

func loadAdminPlaneConfig() (*AdminPlaneConfig, error) {
	server, serverErr := settings.LoadServerConfig()
	valkey, valkeyErr := settings.LoadValkeyConfig()
	postgres, postgresErr := settings.LoadPostgresEndpointConfig()
	notification, notificationErr := settings.LoadNotificationConfig()
	cors, corsErr := settings.LoadCORSConfig(adminCORSDefaultEnforce)
	ingestion, ingestionErr := settings.LoadIngestionConfig()

	if err := errors.Join(
		serverErr, valkeyErr, postgresErr, notificationErr, corsErr, ingestionErr,
	); err != nil {
		return nil, fmt.Errorf("load admin plane sections: %w", err)
	}

	config := &AdminPlaneConfig{
		Environment:      envload.AppEnvironment(),
		Server:           server,
		InternalH3:       settings.LoadInternalH3ClientOptions(),
		Valkey:           valkey,
		Postgres:         postgres,
		Notification:     notification,
		CORS:             cors,
		Ingestion:        ingestion,
		SettingsFilePath: settings.LoadSettingsFilePath(),
		AlarmServiceURL:  sharedenv.String("ALARM_INTERNAL_URL", ""),
	}

	return config, nil
}

// Validate는 admin plane 역할 조건을 검사한다. NonEgress라 Iris 입력 필수 검증은 면제하지만 production CORS 허용 origin을 요구하고, proactive notification egress 소유를 거절한다.
func (c *AdminPlaneConfig) Validate() error {
	if c == nil {
		return errors.New("admin plane config is required")
	}

	if err := settings.ValidateServerRuntime(c.Environment, &c.Server); err != nil {
		return fmt.Errorf("validate server runtime: %w", err)
	}

	if err := runtimepolicy.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := settings.ValidateCORSConfig(c.Environment, c.CORS); err != nil {
		return fmt.Errorf("validate CORS config: %w", err)
	}

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeAdminAPI, sharedenv.String(runtimepolicy.NotificationEgressRoleEnv, ""), sharedenv.String(runtimepolicy.NotificationSchedulerRoleEnv, "")); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}
