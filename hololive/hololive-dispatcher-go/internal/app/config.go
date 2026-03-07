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
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"

	sharedconfig "github.com/kapu/hololive-shared/pkg/config"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	defaultMaxBatch           = 50
	defaultReconnectBackoffMS = 1000
	defaultLoggingLevel       = "info"
	defaultMetricsIntervalSec = 30
)

type envConfig struct {
	DispatcherPort int `envconfig:"DISPATCHER_PORT" default:"30020"`

	IrisBaseURL     string `envconfig:"IRIS_BASE_URL" default:"http://localhost:3000"`
	IrisBotToken    string `envconfig:"IRIS_BOT_TOKEN"`
	IrisSharedToken string `envconfig:"IRIS_SHARED_TOKEN"`

	// CACHE_* 접두사: 나머지 Go 서비스(hololive-shared ValkeyConfig)와 동일 규칙
	ValkeyHost       string `envconfig:"CACHE_HOST"`
	ValkeyPort       string `envconfig:"CACHE_PORT"`
	ValkeyPassword   string `envconfig:"CACHE_PASSWORD"`
	ValkeyDB         string `envconfig:"CACHE_DB"`
	ValkeySocketPath string `envconfig:"CACHE_SOCKET_PATH"`

	// VALKEY_* 접두사: dispatcher 기존 환경변수 (하위호환)
	LegacyValkeyHost       string `envconfig:"VALKEY_HOST"`
	LegacyValkeyPort       string `envconfig:"VALKEY_PORT"`
	LegacyValkeyPassword   string `envconfig:"VALKEY_PASSWORD"`
	LegacyValkeyDB         string `envconfig:"VALKEY_DB"`
	LegacyValkeySocketPath string `envconfig:"VALKEY_SOCKET_PATH"`

	DispatchQueueKey             string `envconfig:"ALARM_DISPATCH_QUEUE_KEY" default:"alarm:dispatch:queue"`
	DispatchMaxBatch             int    `envconfig:"ALARM_DISPATCH_MAX_BATCH" default:"50"`
	DispatcherReconnectBackoffMS int    `envconfig:"DISPATCHER_RECONNECT_BACKOFF_MS" default:"1000"`

	LogLevel      string `envconfig:"LOG_LEVEL" default:"info"`
	LogDir        string `envconfig:"LOG_DIR" default:""`
	LogMaxSizeMB  int    `envconfig:"LOG_MAX_SIZE_MB" default:"100"`
	LogMaxBackups int    `envconfig:"LOG_MAX_BACKUPS" default:"5"`
	LogMaxAgeDays int    `envconfig:"LOG_MAX_AGE_DAYS" default:"30"`
	LogCompress   bool   `envconfig:"LOG_COMPRESS" default:"true"`

	OTELEnabled                  bool    `envconfig:"OTEL_ENABLED" default:"false"`
	OTELMetricsEnabled           bool    `envconfig:"OTEL_METRICS_ENABLED" default:"false"`
	OTELMetricsExportIntervalSec int     `envconfig:"OTEL_METRICS_EXPORT_INTERVAL_SECONDS" default:"30"`
	OTELServiceName              string  `envconfig:"OTEL_SERVICE_NAME" default:"hololive-dispatcher-go"`
	OTELServiceVersion           string  `envconfig:"OTEL_SERVICE_VERSION" default:"1.0.0"`
	OTELEnvironment              string  `envconfig:"OTEL_ENVIRONMENT" default:"production"`
	OTELExporterOTLPEndpoint     string  `envconfig:"OTEL_EXPORTER_OTLP_ENDPOINT" default:"otel-collector:4317"`
	OTELExporterOTLPInsecure     bool    `envconfig:"OTEL_EXPORTER_OTLP_INSECURE" default:"false"`
	OTELSampleRate               float64 `envconfig:"OTEL_SAMPLE_RATE" default:"1.0"`
}

// Config: dispatcher-go 런타임 설정.
type Config struct {
	Server    ServerConfig
	Iris      IrisConfig
	Valkey    cache.Config
	Dispatch  DispatchConfig
	Logging   sharedlogging.Config
	Telemetry sharedconfig.TelemetryConfig
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

	var raw envConfig
	if err := envconfig.Process("", &raw); err != nil {
		return nil, fmt.Errorf("load dispatcher config: process env: %w", err)
	}

	botToken := strings.TrimSpace(raw.IrisBotToken)
	if botToken == "" {
		botToken = strings.TrimSpace(raw.IrisSharedToken)
	}

	maxBatch := raw.DispatchMaxBatch
	if maxBatch <= 0 {
		maxBatch = defaultMaxBatch
	}
	reconnectBackoffMS := raw.DispatcherReconnectBackoffMS
	if reconnectBackoffMS <= 0 {
		reconnectBackoffMS = defaultReconnectBackoffMS
	}
	metricsExportIntervalSec := raw.OTELMetricsExportIntervalSec
	if metricsExportIntervalSec <= 0 {
		metricsExportIntervalSec = defaultMetricsIntervalSec
	}

	cfg := &Config{
		Server: ServerConfig{
			Port: raw.DispatcherPort,
		},
		Iris: IrisConfig{
			BaseURL:  raw.IrisBaseURL,
			BotToken: botToken,
		},
		Valkey: cache.Config{
			Host:       pickTrimmed(raw.ValkeyHost, raw.LegacyValkeyHost, "localhost"),
			Port:       parseIntWithFallback(raw.ValkeyPort, raw.LegacyValkeyPort, 6379),
			Password:   pickTrimmed(raw.ValkeyPassword, raw.LegacyValkeyPassword, ""),
			DB:         parseIntWithFallback(raw.ValkeyDB, raw.LegacyValkeyDB, 0),
			SocketPath: pickTrimmed(raw.ValkeySocketPath, raw.LegacyValkeySocketPath, ""),
		},
		Dispatch: DispatchConfig{
			QueueKey:         strings.TrimSpace(raw.DispatchQueueKey),
			MaxBatch:         maxBatch,
			ReconnectBackoff: time.Duration(reconnectBackoffMS) * time.Millisecond,
		},
		Logging: sharedlogging.Config{
			Level:      strings.TrimSpace(raw.LogLevel),
			Dir:        strings.TrimSpace(raw.LogDir),
			MaxSizeMB:  raw.LogMaxSizeMB,
			MaxBackups: raw.LogMaxBackups,
			MaxAgeDays: raw.LogMaxAgeDays,
			Compress:   raw.LogCompress,
		},
		Telemetry: sharedconfig.TelemetryConfig{
			Enabled:               raw.OTELEnabled,
			MetricsEnabled:        raw.OTELMetricsEnabled,
			MetricsExportInterval: time.Duration(metricsExportIntervalSec) * time.Second,
			ServiceName:           strings.TrimSpace(raw.OTELServiceName),
			ServiceVersion:        strings.TrimSpace(raw.OTELServiceVersion),
			Environment:           strings.TrimSpace(raw.OTELEnvironment),
			OTLPEndpoint:          strings.TrimSpace(raw.OTELExporterOTLPEndpoint),
			OTLPInsecure:          raw.OTELExporterOTLPInsecure,
			SampleRate:            raw.OTELSampleRate,
		},
	}
	if cfg.Dispatch.QueueKey == "" {
		cfg.Dispatch.QueueKey = contractsalarm.DispatchQueueKey
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = defaultLoggingLevel
	}
	if cfg.Telemetry.MetricsExportInterval <= 0 {
		cfg.Telemetry.MetricsExportInterval = defaultMetricsIntervalSec * time.Second
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
