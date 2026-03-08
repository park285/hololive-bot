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
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func setRequiredEnvForLoadConfig(t *testing.T) {
	t.Helper()
	t.Setenv("IRIS_SHARED_TOKEN", "shared-token")
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
	t.Setenv("DISPATCHER_RECONNECT_BACKOFF_MS", "0")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Dispatch.MaxBatch != defaultMaxBatch {
		t.Fatalf("Dispatch.MaxBatch = %d, want %d", cfg.Dispatch.MaxBatch, defaultMaxBatch)
	}
	if cfg.Dispatch.ReconnectBackoff != time.Second {
		t.Fatalf("Dispatch.ReconnectBackoff = %v, want %v", cfg.Dispatch.ReconnectBackoff, time.Second)
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
			QueueKey:         "alarm:dispatch:queue",
			MaxBatch:         50,
			ReconnectBackoff: time.Second,
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
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
			QueueKey:         "alarm:dispatch:queue",
			MaxBatch:         50,
			ReconnectBackoff: time.Second,
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}
