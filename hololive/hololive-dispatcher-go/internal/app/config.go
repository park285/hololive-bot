// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
)

const (
	defaultMaxBatch            = 50
	defaultDispatchParallelism = 4
	defaultReconnectBackoffMS  = 1000
	defaultRetryMaxAttempts    = 3
	defaultRetryBaseBackoff    = 5 * time.Second
	defaultRetryMaxBackoff     = 30 * time.Second
	defaultRetryJitterPercent  = 0.0
	defaultMaxBatchesPerWake   = 20
	defaultLoggingLevel        = "info"
)

type Config struct {
	Server      ServerConfig
	Iris        IrisConfig
	Valkey      cache.Config
	Postgres    PostgresConfig
	Dispatch    DispatchConfig
	Logging     sharedlogging.Config
	Environment string
}

type ServerConfig struct {
	Port int
}

type IrisConfig struct {
	BaseURL     string
	BaseURLFile string
	BotToken    string
}

type PostgresConfig = database.PostgresConfig

type DispatchConfig struct {
	QueueKey           string
	MaxBatch           int
	Parallelism        int
	ReconnectBackoff   time.Duration
	RetryMaxAttempts   int
	RetryBaseBackoff   time.Duration
	RetryMaxBackoff    time.Duration
	RetryJitterPercent float64
	ConsumerMode       string
	PublishMode        string
	LeaseSeconds       int
	PollInterval       time.Duration
	WakeupEnabled      bool
	MaxBatchesPerWake  int
}

func LoadConfig() (*Config, error) {
	_ = godotenv.Load()

	botToken := lookupString("IRIS_BOT_TOKEN", "")
	if botToken == "" {
		botToken = lookupString("IRIS_SHARED_TOKEN", "")
	}

	maxBatch := lookupInt("ALARM_DISPATCH_MAX_BATCH", defaultMaxBatch)
	if maxBatch <= 0 {
		maxBatch = defaultMaxBatch
	}
	parallelism := lookupInt("ALARM_DISPATCH_PARALLELISM", defaultDispatchParallelism)
	if parallelism <= 0 {
		parallelism = defaultDispatchParallelism
	}
	reconnectBackoffMS := lookupInt("DISPATCHER_RECONNECT_BACKOFF_MS", defaultReconnectBackoffMS)
	if reconnectBackoffMS <= 0 {
		reconnectBackoffMS = defaultReconnectBackoffMS
	}
	retryMaxAttempts := lookupInt("ALARM_DISPATCH_RETRY_MAX_ATTEMPTS", defaultRetryMaxAttempts)
	if retryMaxAttempts <= 0 {
		retryMaxAttempts = defaultRetryMaxAttempts
	}
	retryBaseBackoffMS := lookupInt("ALARM_DISPATCH_RETRY_BASE_BACKOFF_MS", int(defaultRetryBaseBackoff/time.Millisecond))
	if retryBaseBackoffMS <= 0 {
		retryBaseBackoffMS = int(defaultRetryBaseBackoff / time.Millisecond)
	}
	retryMaxBackoffMS := lookupInt("ALARM_DISPATCH_RETRY_MAX_BACKOFF_MS", int(defaultRetryMaxBackoff/time.Millisecond))
	if retryMaxBackoffMS <= 0 {
		retryMaxBackoffMS = int(defaultRetryMaxBackoff / time.Millisecond)
	}
	retryJitterPercent := lookupFloat("ALARM_DISPATCH_RETRY_JITTER_PERCENT", defaultRetryJitterPercent)
	if isFiniteFloat(retryJitterPercent) && retryJitterPercent < 0 {
		retryJitterPercent = defaultRetryJitterPercent
	}
	cfg := &Config{
		Server: ServerConfig{
			Port: lookupInt("DISPATCHER_PORT", 30020),
		},
		Iris: IrisConfig{
			BaseURL:     lookupString("IRIS_BASE_URL", ""),
			BaseURLFile: lookupString("IRIS_BASE_URL_FILE", ""),
			BotToken:    botToken,
		},
		Valkey: cache.Config{
			Host:       pickTrimmed(lookupOptional("CACHE_HOST"), lookupOptional("VALKEY_HOST"), "localhost"),
			Port:       parseIntWithFallback(lookupOptional("CACHE_PORT"), lookupOptional("VALKEY_PORT"), 6379),
			Password:   pickTrimmed(lookupOptional("CACHE_PASSWORD"), lookupOptional("VALKEY_PASSWORD"), ""),
			DB:         parseIntWithFallback(lookupOptional("CACHE_DB"), lookupOptional("VALKEY_DB"), 0),
			SocketPath: pickTrimmed(lookupOptional("CACHE_SOCKET_PATH"), lookupOptional("VALKEY_SOCKET_PATH"), ""),
		},
		Postgres: PostgresConfig{
			Host:          lookupString("POSTGRES_HOST", "localhost"),
			Port:          lookupInt("POSTGRES_PORT", 5432),
			SocketPath:    lookupString("POSTGRES_SOCKET_PATH", ""),
			User:          lookupString("POSTGRES_USER", ""),
			Password:      lookupString("POSTGRES_PASSWORD", ""),
			Database:      lookupString("POSTGRES_DB", ""),
			SSLMode:       lookupString("POSTGRES_SSLMODE", "require"),
			QueryExecMode: lookupString("POSTGRES_QUERY_EXEC_MODE", "cache_statement"),
			PoolMinConns:  lookupInt("POSTGRES_POOL_MIN_CONNS", 1),
			PoolMaxConns:  lookupInt("POSTGRES_POOL_MAX_CONNS", 4),
		},
		Dispatch: DispatchConfig{
			QueueKey:           lookupString("ALARM_DISPATCH_QUEUE_KEY", "alarm:dispatch:queue"),
			MaxBatch:           maxBatch,
			Parallelism:        parallelism,
			ReconnectBackoff:   time.Duration(reconnectBackoffMS) * time.Millisecond,
			RetryMaxAttempts:   retryMaxAttempts,
			RetryBaseBackoff:   time.Duration(retryBaseBackoffMS) * time.Millisecond,
			RetryMaxBackoff:    time.Duration(retryMaxBackoffMS) * time.Millisecond,
			RetryJitterPercent: retryJitterPercent,
			ConsumerMode:       lookupString("ALARM_DISPATCH_CONSUMER_MODE", "valkey"),
			PublishMode:        lookupString("ALARM_DISPATCH_PUBLISH_MODE", ""),
			LeaseSeconds:       lookupInt("ALARM_DISPATCH_LEASE_SECONDS", 60),
			PollInterval:       time.Duration(lookupInt("ALARM_DISPATCH_POLL_INTERVAL_MS", 1000)) * time.Millisecond,
			WakeupEnabled:      lookupBool("ALARM_DISPATCH_WAKEUP_ENABLED", true),
			MaxBatchesPerWake:  lookupInt("ALARM_DISPATCH_MAX_BATCHES_PER_WAKE", defaultMaxBatchesPerWake),
		},
		Logging: sharedlogging.Config{
			Level:      lookupString("LOG_LEVEL", "info"),
			Dir:        lookupString("LOG_DIR", ""),
			MaxSizeMB:  lookupInt("LOG_MAX_SIZE_MB", 100),
			MaxBackups: lookupInt("LOG_MAX_BACKUPS", 5),
			MaxAgeDays: lookupInt("LOG_MAX_AGE_DAYS", 30),
			Compress:   lookupBool("LOG_COMPRESS", true),
		},
		Environment: lookupString("APP_ENV", "production"),
	}
	if cfg.Dispatch.QueueKey == "" {
		cfg.Dispatch.QueueKey = contractsalarm.DispatchQueueKey
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = defaultLoggingLevel
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("load dispatcher config: validate: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.Port <= 0 {
		return fmt.Errorf("validate config: DISPATCHER_PORT must be positive")
	}
	if strings.TrimSpace(c.Iris.BaseURL) == "" && strings.TrimSpace(c.Iris.BaseURLFile) == "" {
		return fmt.Errorf("validate config: IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
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
	if c.Dispatch.Parallelism <= 0 {
		return fmt.Errorf("validate config: ALARM_DISPATCH_PARALLELISM must be positive")
	}
	if c.Dispatch.ReconnectBackoff <= 0 {
		return fmt.Errorf("validate config: DISPATCHER_RECONNECT_BACKOFF_MS must be positive")
	}
	if c.Dispatch.RetryMaxAttempts <= 0 {
		return fmt.Errorf("validate config: ALARM_DISPATCH_RETRY_MAX_ATTEMPTS must be positive")
	}
	if c.Dispatch.RetryBaseBackoff <= 0 {
		return fmt.Errorf("validate config: ALARM_DISPATCH_RETRY_BASE_BACKOFF_MS must be positive")
	}
	if c.Dispatch.RetryMaxBackoff <= 0 {
		return fmt.Errorf("validate config: ALARM_DISPATCH_RETRY_MAX_BACKOFF_MS must be positive")
	}
	if c.Dispatch.RetryMaxBackoff < c.Dispatch.RetryBaseBackoff {
		return fmt.Errorf("validate config: ALARM_DISPATCH_RETRY_MAX_BACKOFF_MS must be greater than or equal to ALARM_DISPATCH_RETRY_BASE_BACKOFF_MS")
	}
	if !isFiniteFloat(c.Dispatch.RetryJitterPercent) {
		return fmt.Errorf("validate config: ALARM_DISPATCH_RETRY_JITTER_PERCENT must be finite")
	}
	if c.Dispatch.RetryJitterPercent < 0 || c.Dispatch.RetryJitterPercent > 100 {
		return fmt.Errorf("validate config: ALARM_DISPATCH_RETRY_JITTER_PERCENT must be between 0 and 100")
	}
	switch strings.ToLower(strings.TrimSpace(c.Dispatch.ConsumerMode)) {
	case "", "valkey":
		c.Dispatch.ConsumerMode = "valkey"
	case "pg":
		c.Dispatch.ConsumerMode = "pg"
	default:
		return fmt.Errorf("validate config: ALARM_DISPATCH_CONSUMER_MODE must be valkey or pg")
	}
	if err := validateAlarmDispatchModePair(c.Dispatch.PublishMode, c.Dispatch.ConsumerMode); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	if c.Dispatch.LeaseSeconds <= 0 {
		c.Dispatch.LeaseSeconds = 60
	}
	if c.Dispatch.PollInterval <= 0 {
		c.Dispatch.PollInterval = time.Second
	}
	if c.Dispatch.MaxBatchesPerWake <= 0 {
		c.Dispatch.MaxBatchesPerWake = defaultMaxBatchesPerWake
	}
	if c.Dispatch.ConsumerMode == "pg" {
		if strings.TrimSpace(c.Postgres.SocketPath) == "" && strings.TrimSpace(c.Postgres.Host) == "" {
			return fmt.Errorf("validate config: POSTGRES_HOST is required in pg consumer mode when POSTGRES_SOCKET_PATH is empty")
		}
		if strings.TrimSpace(c.Postgres.User) == "" {
			return fmt.Errorf("validate config: POSTGRES_USER is required in pg consumer mode")
		}
		if strings.TrimSpace(c.Postgres.Database) == "" {
			return fmt.Errorf("validate config: POSTGRES_DB is required in pg consumer mode")
		}
	}
	if c.Dispatch.ConsumerMode == "valkey" && strings.TrimSpace(c.Valkey.SocketPath) == "" && strings.TrimSpace(c.Valkey.Host) == "" {
		return fmt.Errorf("validate config: CACHE_HOST is required when CACHE_SOCKET_PATH is empty")
	}
	return nil
}

func validateAlarmDispatchModePair(rawPublishMode string, consumerMode string) error {
	publishMode := queue.PublishMode(strings.ToLower(strings.TrimSpace(rawPublishMode)))
	if publishMode == "" {
		if consumerMode == "pg" {
			return fmt.Errorf("ALARM_DISPATCH_PUBLISH_MODE is required when ALARM_DISPATCH_CONSUMER_MODE=pg")
		}
		return nil
	}
	switch publishMode {
	case queue.PublishModeValkeyOnly, queue.PublishModeShadow, queue.PublishModePGFirst:
	default:
		return fmt.Errorf("ALARM_DISPATCH_PUBLISH_MODE must be valkey_only, shadow, or pg_first when provided")
	}
	if publishMode == queue.PublishModePGFirst && consumerMode != "pg" {
		return fmt.Errorf("forbidden alarm dispatch mode combination: publisher=pg_first requires consumer=pg")
	}
	if publishMode != queue.PublishModePGFirst && consumerMode == "pg" {
		return fmt.Errorf("forbidden alarm dispatch mode combination: consumer=pg requires publisher=pg_first")
	}
	return nil
}

func pickTrimmed(primary, secondary, def string) string {
	if trimmed := strings.TrimSpace(primary); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(secondary); trimmed != "" {
		return trimmed
	}
	return def
}

func parseIntWithFallback(primary, secondary string, def int) int {
	raw := pickTrimmed(primary, secondary, "")
	if raw == "" {
		return def
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return parsed
}

func lookupOptional(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func lookupString(key, def string) string {
	if value := lookupOptional(key); value != "" {
		return value
	}
	return def
}

func lookupInt(key string, def int) int {
	raw := lookupOptional(key)
	if raw == "" {
		return def
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return parsed
}

func lookupFloat(key string, def float64) float64 {
	raw := lookupOptional(key)
	if raw == "" {
		return def
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return parsed
}

func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func lookupBool(key string, def bool) bool {
	raw := lookupOptional(key)
	if raw == "" {
		return def
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return parsed
}
