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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
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
	defaultLoggingLevel        = "info"
)

type Config struct {
	Server      ServerConfig
	Iris        IrisConfig
	Valkey      cache.Config
	Dispatch    DispatchConfig
	Logging     sharedlogging.Config
	Environment string
}

type ServerConfig struct {
	Port int
}

type IrisConfig struct {
	BaseURL  string
	BotToken string
}

type DispatchConfig struct {
	QueueKey           string
	MaxBatch           int
	Parallelism        int
	ReconnectBackoff   time.Duration
	RetryMaxAttempts   int
	RetryBaseBackoff   time.Duration
	RetryMaxBackoff    time.Duration
	RetryJitterPercent float64
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
	if retryJitterPercent < 0 {
		retryJitterPercent = defaultRetryJitterPercent
	}
	cfg := &Config{
		Server: ServerConfig{
			Port: lookupInt("DISPATCHER_PORT", 30020),
		},
		Iris: IrisConfig{
			BaseURL:  lookupString("IRIS_BASE_URL", "http://localhost:3000"),
			BotToken: botToken,
		},
		Valkey: cache.Config{
			Host:       pickTrimmed(lookupOptional("CACHE_HOST"), lookupOptional("VALKEY_HOST"), "localhost"),
			Port:       parseIntWithFallback(lookupOptional("CACHE_PORT"), lookupOptional("VALKEY_PORT"), 6379),
			Password:   pickTrimmed(lookupOptional("CACHE_PASSWORD"), lookupOptional("VALKEY_PASSWORD"), ""),
			DB:         parseIntWithFallback(lookupOptional("CACHE_DB"), lookupOptional("VALKEY_DB"), 0),
			SocketPath: pickTrimmed(lookupOptional("CACHE_SOCKET_PATH"), lookupOptional("VALKEY_SOCKET_PATH"), ""),
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
	if c.Dispatch.RetryJitterPercent < 0 || c.Dispatch.RetryJitterPercent > 100 {
		return fmt.Errorf("validate config: ALARM_DISPATCH_RETRY_JITTER_PERCENT must be between 0 and 100")
	}
	if strings.TrimSpace(c.Valkey.SocketPath) == "" && strings.TrimSpace(c.Valkey.Host) == "" {
		return fmt.Errorf("validate config: CACHE_HOST is required when CACHE_SOCKET_PATH is empty")
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
