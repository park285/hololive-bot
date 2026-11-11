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

// AdminPlaneConfig는 admin plane(관리 HTTP API·room ACL 관리·운영 상태 조회)이 소비하는 설정만 담는다.
// Holodex·공식 일정 설정은 process 하나의 Holodex 서비스를 만드는 bot plane 설정이 소유한다.
// Admin plane은 compose 보안 계약상 nonEgress라 Iris egress 입력을 갖지 않는다.
type AdminPlaneConfig struct {
	Environment  string
	Server       settings.ServerConfig
	InternalH3   sharedh3.ClientOptions
	Postgres     settings.PostgresConfig
	Notification settings.NotificationConfig
	Ingestion    IngestionConfig
	// SettingsFilePath: 관리 화면이 저장하는 persisted settings(JSON) 경로. SETTINGS_DIR(기본 data)/settings.json.
	SettingsFilePath string
	// AlarmServiceURL은 bot plane이 읽은 ALARM_INTERNAL_URL을 configurePlanes가 그대로 옮긴 값이다.
	AlarmServiceURL string
	// XSpacesKeyFile: X_SPACES_KEY_FILE. 비어 있으면 X 세션 연결 제출이 꺼진다. 키 파일 검증은 xspaces.LoadStore가 한다.
	XSpacesKeyFile string
}

func loadAdminPlaneConfig() (*AdminPlaneConfig, error) {
	server, serverErr := settings.LoadServerConfig()
	postgres, postgresErr := settings.LoadPostgresEndpointConfig()
	notification, notificationErr := settings.LoadNotificationConfig()
	ingestion, ingestionErr := loadIngestionConfig()

	if err := errors.Join(
		serverErr, postgresErr, notificationErr, ingestionErr,
	); err != nil {
		return nil, fmt.Errorf("load admin plane sections: %w", err)
	}

	config := &AdminPlaneConfig{
		Environment:      envload.AppEnvironment(),
		Server:           server,
		InternalH3:       loadInternalH3ClientOptions(),
		Postgres:         postgres,
		Notification:     notification,
		Ingestion:        ingestion,
		SettingsFilePath: settings.LoadSettingsFilePath(),
		XSpacesKeyFile:   sharedenv.StringRaw("X_SPACES_KEY_FILE", ""),
	}

	return config, nil
}

type IngestionConfig struct {
	PhotoSyncEnabled bool
}

// loadIngestionConfig는 PHOTO_SYNC_ENABLED 미설정을 true로 본다. 같은 키를 자기 loader로 읽는 collector는 기본값이 false다.
func loadIngestionConfig() (IngestionConfig, error) {
	photoSyncEnabled, err := sharedenv.BoolE("PHOTO_SYNC_ENABLED", true)
	if err != nil {
		return IngestionConfig{}, fmt.Errorf("load ingestion config: %w", err)
	}

	return IngestionConfig{PhotoSyncEnabled: photoSyncEnabled}, nil
}

// Validate는 admin plane 역할 조건을 검사한다. NonEgress라 Iris 입력 필수 검증은 면제하고 proactive notification egress 소유를 거절한다.
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

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeAdminAPI, sharedenv.String(runtimepolicy.NotificationEgressRoleEnv, ""), sharedenv.String(runtimepolicy.NotificationSchedulerRoleEnv, "")); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}
