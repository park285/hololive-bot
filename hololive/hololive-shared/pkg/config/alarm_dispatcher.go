package config

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// AlarmDispatcherConfig: alarm-dispatcher 바이너리 전용 설정
type AlarmDispatcherConfig struct {
	Iris         IrisConfig
	Valkey       ValkeyConfig
	Postgres     PostgresConfig
	Notification NotificationConfig
	Holodex      HolodexConfig
	Chzzk        ChzzkConfig
	Twitch       TwitchConfig
	Logging      LoggingConfig
	Scraper      ScraperConfig
	Port         int
	Version      string
}

// LoadAlarmDispatcher: alarm-dispatcher 전용 설정을 환경변수에서 로드합니다.
func LoadAlarmDispatcher() (*AlarmDispatcherConfig, error) {
	_ = godotenv.Load()

	cfg := buildAlarmDispatcherConfig()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("alarm dispatcher config validation failed: %w", err)
	}

	return cfg, nil
}

func buildAlarmDispatcherConfig() *AlarmDispatcherConfig {
	return &AlarmDispatcherConfig{
		Iris: IrisConfig{
			BaseURL:  envutil.String("IRIS_BASE_URL", "http://localhost:3000"),
			BotToken: envutil.String("IRIS_BOT_TOKEN", envutil.String("IRIS_SHARED_TOKEN", "")),
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Notification: NotificationConfig{
			AdvanceMinutes:            parseIntList(envutil.String("NOTIFICATION_ADVANCE_MINUTES", "5")),
			AlarmQueueConsumerEnabled: envutil.Bool("GO_ALARM_QUEUE_CONSUMER_ENABLED", true),
		},
		Holodex: HolodexConfig{
			BaseURL: envutil.String("HOLODEX_BASE_URL", constants.APIConfig.HolodexBaseURL),
			APIKeys: collectAPIKeys("HOLODEX_API_KEY_"),
		},
		Chzzk: ChzzkConfig{
			ClientID:     envutil.String("CHZZK_CLIENT_ID", ""),
			ClientSecret: envutil.String("CHZZK_CLIENT_SECRET", ""),
		},
		Twitch: TwitchConfig{
			ClientID:     envutil.String("TWITCH_CLIENT_ID", ""),
			ClientSecret: envutil.String("TWITCH_CLIENT_SECRET", ""),
		},
		Logging: LoggingConfig{
			Level:      envutil.String("LOG_LEVEL", "info"),
			Dir:        envutil.String("LOG_DIR", ""),
			MaxSizeMB:  envutil.Int("LOG_MAX_SIZE_MB", 100),
			MaxBackups: envutil.Int("LOG_MAX_BACKUPS", 5),
			MaxAgeDays: envutil.Int("LOG_MAX_AGE_DAYS", 30),
			Compress:   envutil.Bool("LOG_COMPRESS", true),
		},
		Scraper: ScraperConfig{
			ProxyEnabled: envutil.Bool("SCRAPER_PROXY_ENABLED", false),
			ProxyURL:     envutil.String("SCRAPER_PROXY_URL", ""),
		},
		Port:    envutil.Int("ALARM_DISPATCHER_PORT", 30010),
		Version: envutil.String("APP_VERSION", "1.0.0-dispatcher"),
	}
}

// validate: 필수 설정값을 검증합니다.
func (c *AlarmDispatcherConfig) validate() error {
	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return fmt.Errorf("IRIS_BOT_TOKEN (or IRIS_SHARED_TOKEN) is required")
	}
	if len(c.Holodex.APIKeys) == 0 {
		return fmt.Errorf("at least one HOLODEX_API_KEY is required")
	}
	return nil
}
