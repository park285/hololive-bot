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
	"math"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func setRequiredEnvForLoadConfig(t *testing.T) {
	t.Helper()
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
	t.Setenv("IRIS_BASE_URL_FILE", "/tmp/iris_base_url")
}

func TestLoadConfig_UsesIRISSharedTokenFallback(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("IRIS_BOT_TOKEN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Iris.BotToken != "shared-token" {
		t.Fatalf("Iris.BotToken = %q, want %q", cfg.Iris.BotToken, "shared-token")
	}
}

func TestLoadConfig_IgnoresLegacyTelemetryEnvironment(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("APP_ENV", "")
	t.Setenv("OTEL_ENVIRONMENT", "development")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Environment != "production" {
		t.Fatalf("Environment = %q, want %q", cfg.Environment, "production")
	}
}

func TestLoadConfig_AppliesDefaultWhenInvalidNumericValue(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("ALARM_DISPATCH_MAX_BATCH", "-1")
	t.Setenv("ALARM_DISPATCH_PARALLELISM", "0")
	t.Setenv("DISPATCHER_RECONNECT_BACKOFF_MS", "0")
	t.Setenv("ALARM_DISPATCH_RETRY_MAX_ATTEMPTS", "0")
	t.Setenv("ALARM_DISPATCH_RETRY_BASE_BACKOFF_MS", "0")
	t.Setenv("ALARM_DISPATCH_RETRY_MAX_BACKOFF_MS", "-5")
	t.Setenv("ALARM_DISPATCH_RETRY_JITTER_PERCENT", "-1")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Dispatch.MaxBatch != defaultMaxBatch {
		t.Fatalf("Dispatch.MaxBatch = %d, want %d", cfg.Dispatch.MaxBatch, defaultMaxBatch)
	}
	if cfg.Dispatch.Parallelism != defaultDispatchParallelism {
		t.Fatalf("Dispatch.Parallelism = %d, want %d", cfg.Dispatch.Parallelism, defaultDispatchParallelism)
	}
	if cfg.Dispatch.ReconnectBackoff != time.Second {
		t.Fatalf("Dispatch.ReconnectBackoff = %v, want %v", cfg.Dispatch.ReconnectBackoff, time.Second)
	}
	if cfg.Dispatch.RetryMaxAttempts != defaultRetryMaxAttempts {
		t.Fatalf("Dispatch.RetryMaxAttempts = %d, want %d", cfg.Dispatch.RetryMaxAttempts, defaultRetryMaxAttempts)
	}
	if cfg.Dispatch.RetryBaseBackoff != defaultRetryBaseBackoff {
		t.Fatalf("Dispatch.RetryBaseBackoff = %v, want %v", cfg.Dispatch.RetryBaseBackoff, defaultRetryBaseBackoff)
	}
	if cfg.Dispatch.RetryMaxBackoff != defaultRetryMaxBackoff {
		t.Fatalf("Dispatch.RetryMaxBackoff = %v, want %v", cfg.Dispatch.RetryMaxBackoff, defaultRetryMaxBackoff)
	}
	if cfg.Dispatch.RetryJitterPercent != defaultRetryJitterPercent {
		t.Fatalf("Dispatch.RetryJitterPercent = %v, want %v", cfg.Dispatch.RetryJitterPercent, defaultRetryJitterPercent)
	}
}

func TestLoadConfig_LegacyValkeyEnvFallback(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("CACHE_HOST", "")
	t.Setenv("CACHE_PORT", "")
	t.Setenv("VALKEY_HOST", "legacy-valkey")
	t.Setenv("VALKEY_PORT", "6381")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Valkey.Host != "legacy-valkey" {
		t.Fatalf("Valkey.Host = %q, want %q", cfg.Valkey.Host, "legacy-valkey")
	}
	if cfg.Valkey.Port != 6381 {
		t.Fatalf("Valkey.Port = %d, want %d", cfg.Valkey.Port, 6381)
	}
}

func TestLoadConfig_RetryPolicyOverrides(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("ALARM_DISPATCH_RETRY_MAX_ATTEMPTS", "7")
	t.Setenv("ALARM_DISPATCH_RETRY_BASE_BACKOFF_MS", "1500")
	t.Setenv("ALARM_DISPATCH_RETRY_MAX_BACKOFF_MS", "30000")
	t.Setenv("ALARM_DISPATCH_RETRY_JITTER_PERCENT", "12.5")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Dispatch.RetryMaxAttempts != 7 {
		t.Fatalf("Dispatch.RetryMaxAttempts = %d, want 7", cfg.Dispatch.RetryMaxAttempts)
	}
	if cfg.Dispatch.RetryBaseBackoff != 1500*time.Millisecond {
		t.Fatalf("Dispatch.RetryBaseBackoff = %v, want %v", cfg.Dispatch.RetryBaseBackoff, 1500*time.Millisecond)
	}
	if cfg.Dispatch.RetryMaxBackoff != 30*time.Second {
		t.Fatalf("Dispatch.RetryMaxBackoff = %v, want %v", cfg.Dispatch.RetryMaxBackoff, 30*time.Second)
	}
	if cfg.Dispatch.RetryJitterPercent != 12.5 {
		t.Fatalf("Dispatch.RetryJitterPercent = %v, want 12.5", cfg.Dispatch.RetryJitterPercent)
	}
}

func TestLoadConfig_RecoveryOverrides(t *testing.T) {
	setRequiredEnvForLoadConfig(t)
	t.Setenv("ALARM_DISPATCH_RECOVERY_INTERVAL_MS", "5000")
	t.Setenv("ALARM_DISPATCH_RECOVERY_BATCH_SIZE", "250")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Dispatch.RecoveryInterval != 5*time.Second {
		t.Fatalf("Dispatch.RecoveryInterval = %v, want 5s", cfg.Dispatch.RecoveryInterval)
	}
	if cfg.Dispatch.RecoveryBatchSize != 250 {
		t.Fatalf("Dispatch.RecoveryBatchSize = %d, want 250", cfg.Dispatch.RecoveryBatchSize)
	}
}

func TestLoadConfig_RejectsNonFiniteRetryJitter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "nan", value: "NaN"},
		{name: "positive infinity", value: "+Inf"},
		{name: "negative infinity", value: "-Inf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnvForLoadConfig(t)
			t.Setenv("ALARM_DISPATCH_RETRY_JITTER_PERCENT", tc.value)

			if _, err := LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() with jitter %q expected error, got nil", tc.value)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL:  "http://localhost:3000",
			BotToken: "token",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Dispatch: DispatchConfig{
			QueueKey:           "alarm:dispatch:queue",
			MaxBatch:           50,
			Parallelism:        4,
			ReconnectBackoff:   time.Second,
			RetryMaxAttempts:   3,
			RetryBaseBackoff:   5 * time.Second,
			RetryMaxBackoff:    30 * time.Second,
			RetryJitterPercent: 20,
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidate_RejectsForbiddenPublishConsumerModePair(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL:  "http://localhost:3000",
			BotToken: "token",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Dispatch: DispatchConfig{
			QueueKey:           "alarm:dispatch:queue",
			MaxBatch:           50,
			Parallelism:        4,
			ReconnectBackoff:   time.Second,
			RetryMaxAttempts:   3,
			RetryBaseBackoff:   5 * time.Second,
			RetryMaxBackoff:    30 * time.Second,
			RetryJitterPercent: 20,
			ConsumerMode:       "pg",
			PublishMode:        "shadow",
		},
		Postgres: PostgresConfig{
			Host:     "localhost",
			User:     "hololive",
			Database: "hololive",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected forbidden mode pair validation error, got nil")
	}
}

func TestConfigValidate_RejectsPGConsumerWithoutPeerPublishMode(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL:  "http://localhost:3000",
			BotToken: "token",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Postgres: PostgresConfig{
			Host:     "localhost",
			User:     "hololive",
			Database: "hololive",
		},
		Dispatch: DispatchConfig{
			QueueKey:           "alarm:dispatch:queue",
			MaxBatch:           50,
			Parallelism:        4,
			ReconnectBackoff:   time.Second,
			RetryMaxAttempts:   3,
			RetryBaseBackoff:   5 * time.Second,
			RetryMaxBackoff:    30 * time.Second,
			RetryJitterPercent: 20,
			ConsumerMode:       "pg",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing peer publish mode validation error, got nil")
	} else if !strings.Contains(err.Error(), "ALARM_DISPATCH_PUBLISH_MODE is required") {
		t.Fatalf("validation error = %q, want missing publish mode", err.Error())
	}
}

func TestConfigValidate_RequiresBotToken(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL: "http://localhost:3000",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Dispatch: DispatchConfig{
			QueueKey:           "alarm:dispatch:queue",
			MaxBatch:           50,
			Parallelism:        4,
			ReconnectBackoff:   time.Second,
			RetryMaxAttempts:   3,
			RetryBaseBackoff:   5 * time.Second,
			RetryMaxBackoff:    30 * time.Second,
			RetryJitterPercent: 20,
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}

func TestConfigValidate_RequiresValidRetryPolicy(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Server: ServerConfig{Port: 30020},
		Iris: IrisConfig{
			BaseURL:  "http://localhost:3000",
			BotToken: "token",
		},
		Valkey: cache.Config{
			Host: "localhost",
			Port: 6379,
		},
		Dispatch: DispatchConfig{
			QueueKey:           "alarm:dispatch:queue",
			MaxBatch:           50,
			Parallelism:        4,
			ReconnectBackoff:   time.Second,
			RetryMaxAttempts:   3,
			RetryBaseBackoff:   5 * time.Second,
			RetryMaxBackoff:    4 * time.Second,
			RetryJitterPercent: 101,
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestConfigValidate_RejectsNonFiniteRetryJitter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value float64
	}{
		{name: "nan", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{Port: 30020},
				Iris: IrisConfig{
					BaseURL:  "http://localhost:3000",
					BotToken: "token",
				},
				Valkey: cache.Config{
					Host: "localhost",
					Port: 6379,
				},
				Dispatch: DispatchConfig{
					QueueKey:           "alarm:dispatch:queue",
					MaxBatch:           50,
					Parallelism:        4,
					ReconnectBackoff:   time.Second,
					RetryMaxAttempts:   3,
					RetryBaseBackoff:   5 * time.Second,
					RetryMaxBackoff:    30 * time.Second,
					RetryJitterPercent: tc.value,
				},
			}

			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() with jitter %v expected error, got nil", tc.value)
			}
		})
	}
}
