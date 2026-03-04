package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	defaultDispatcherPort = 30020
	defaultValkeyHost     = "localhost"
	defaultValkeyPort     = 6379
	defaultValkeyDB       = 0
	defaultMaxBatch       = 50
)

// Config: dispatcher-go 런타임 설정.
type Config struct {
	Server   ServerConfig
	Iris     IrisConfig
	Valkey   cache.Config
	Dispatch DispatchConfig
	Logging  sharedlogging.Config
}

// ServerConfig: HTTP 서버 설정.
type ServerConfig struct {
	Port int
}

// IrisConfig: Iris 메시지 전송 설정.
type IrisConfig struct {
	BaseURL  string
	BotToken string
}

// DispatchConfig: 큐 소비 및 디스패치 설정.
type DispatchConfig struct {
	QueueKey         string
	MaxBatch         int
	ReconnectBackoff time.Duration
}

// LoadConfig: 환경 변수에서 dispatcher-go 설정을 로드한다.
func LoadConfig() (*Config, error) {
	_ = godotenv.Load()

	botToken := strings.TrimSpace(envString("IRIS_BOT_TOKEN", ""))
	if botToken == "" {
		botToken = strings.TrimSpace(envString("IRIS_SHARED_TOKEN", ""))
	}

	maxBatch := envInt("ALARM_DISPATCH_MAX_BATCH", defaultMaxBatch)
	if maxBatch <= 0 {
		maxBatch = defaultMaxBatch
	}

	cfg := &Config{
		Server: ServerConfig{
			Port: envInt("DISPATCHER_PORT", defaultDispatcherPort),
		},
		Iris: IrisConfig{
			BaseURL:  envString("IRIS_BASE_URL", "http://localhost:3000"),
			BotToken: botToken,
		},
		Valkey: cache.Config{
			Host:       envString("VALKEY_HOST", defaultValkeyHost),
			Port:       envInt("VALKEY_PORT", defaultValkeyPort),
			Password:   envString("VALKEY_PASSWORD", ""),
			DB:         envInt("VALKEY_DB", defaultValkeyDB),
			SocketPath: envString("VALKEY_SOCKET_PATH", ""),
		},
		Dispatch: DispatchConfig{
			QueueKey:         envString("ALARM_DISPATCH_QUEUE_KEY", contractsalarm.DispatchQueueKey),
			MaxBatch:         maxBatch,
			ReconnectBackoff: time.Duration(envInt("DISPATCHER_RECONNECT_BACKOFF_MS", 1000)) * time.Millisecond,
		},
		Logging: sharedlogging.Config{
			Level:      envString("LOG_LEVEL", "info"),
			Dir:        envString("LOG_DIR", ""),
			MaxSizeMB:  envInt("LOG_MAX_SIZE_MB", 100),
			MaxBackups: envInt("LOG_MAX_BACKUPS", 5),
			MaxAgeDays: envInt("LOG_MAX_AGE_DAYS", 30),
			Compress:   envBool("LOG_COMPRESS", true),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("load dispatcher config: validate: %w", err)
	}

	return cfg, nil
}

// Validate: 설정값 기본 검증.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 {
		return fmt.Errorf("validate config: DISPATCHER_PORT must be positive")
	}
	if strings.TrimSpace(c.Iris.BaseURL) == "" {
		return fmt.Errorf("validate config: IRIS_BASE_URL is required")
	}
	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return fmt.Errorf("validate config: IRIS_BOT_TOKEN or IRIS_SHARED_TOKEN is required")
	}
	if strings.TrimSpace(c.Dispatch.QueueKey) == "" {
		return fmt.Errorf("validate config: ALARM_DISPATCH_QUEUE_KEY is required")
	}
	if c.Dispatch.MaxBatch <= 0 {
		return fmt.Errorf("validate config: ALARM_DISPATCH_MAX_BATCH must be positive")
	}
	if c.Dispatch.ReconnectBackoff <= 0 {
		return fmt.Errorf("validate config: DISPATCHER_RECONNECT_BACKOFF_MS must be positive")
	}
	if strings.TrimSpace(c.Valkey.SocketPath) == "" && strings.TrimSpace(c.Valkey.Host) == "" {
		return fmt.Errorf("validate config: VALKEY_HOST is required when VALKEY_SOCKET_PATH is empty")
	}
	return nil
}

func envString(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return defaultValue
}

func envInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func envBool(key string, defaultValue bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}
