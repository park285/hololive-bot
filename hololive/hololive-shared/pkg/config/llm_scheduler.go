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

package config

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"

	"github.com/kapu/hololive-shared/internal/envutil"
)

// LLMSchedulerConfig: llm-scheduler 바이너리 전용 설정
type LLMSchedulerConfig struct {
	Server      ServerConfig
	Iris        IrisConfig
	Valkey      ValkeyConfig
	Postgres    PostgresConfig
	Logging     LoggingConfig
	Bot         BotConfig
	Environment string
	Cliproxy    CliproxyConfig
	LLM         LLMConfig
	Exa         ExaConfig
	Version     string
}

// LoadLLMScheduler: llm-scheduler 전용 설정을 환경변수에서 로드합니다.
func LoadLLMScheduler() (*LLMSchedulerConfig, error) {
	_ = godotenv.Load()

	cfg := buildLLMSchedulerConfig()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("llm scheduler config validation failed: %w", err)
	}
	return cfg, nil
}

func buildLLMSchedulerConfig() *LLMSchedulerConfig {
	webhookToken, botToken, _, _ := loadRuntimeTokensAndCORS()

	return &LLMSchedulerConfig{
		Server: ServerConfig{
			Port:   envutil.Int("LLM_SCHEDULER_PORT", 30003),
			APIKey: envutil.String("API_SECRET_KEY", ""),
		},
		Iris: IrisConfig{
			BaseURL:      envutil.String("IRIS_BASE_URL", "http://localhost:3000"),
			WebhookToken: webhookToken,
			BotToken:     botToken,
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Logging: LoggingConfig{
			Level:      envutil.String("LOG_LEVEL", "info"),
			Dir:        envutil.String("LOG_DIR", ""),
			MaxSizeMB:  envutil.Int("LOG_MAX_SIZE_MB", 100),
			MaxBackups: envutil.Int("LOG_MAX_BACKUPS", 5),
			MaxAgeDays: envutil.Int("LOG_MAX_AGE_DAYS", 30),
			Compress:   envutil.Bool("LOG_COMPRESS", true),
		},
		Bot: BotConfig{
			Prefix:   envutil.String("BOT_PREFIX", "!"),
			SelfUser: envutil.String("BOT_SELF_USER", "iris"),
		},
		Environment: loadAppEnvironment(),
		Cliproxy:    loadCliproxyConfig(),
		LLM:         loadLLMConfig(),
		Exa:         loadExaConfig(),
		Version:     envutil.String("APP_VERSION", "1.0.0-llm-scheduler"),
	}
}

func (c *LLMSchedulerConfig) validate() error {
	if c.Server.Port == 0 {
		return fmt.Errorf("LLM_SCHEDULER_PORT is required")
	}
	if err := validateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return err
	}
	if strings.TrimSpace(c.Iris.WebhookToken) == "" {
		return fmt.Errorf("IRIS_WEBHOOK_TOKEN (or IRIS_SHARED_TOKEN) is required")
	}
	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return fmt.Errorf("IRIS_BOT_TOKEN (or IRIS_SHARED_TOKEN) is required")
	}
	if err := validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	return nil
}
