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

package settings

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

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

func LoadLLMScheduler() (*LLMSchedulerConfig, error) {
	return loadLLMSchedulerValidated((*LLMSchedulerConfig).validate)
}

// LoadLLMSchedulerRuntime: llm-scheduler는 compose 보안 계약상 nonEgress라
// Iris egress 토큰을 받을 수 없으므로 Iris 입력 필수 검증을 면제합니다.
func LoadLLMSchedulerRuntime() (*LLMSchedulerConfig, error) {
	return loadLLMSchedulerValidated((*LLMSchedulerConfig).validateRuntime)
}

func loadLLMSchedulerValidated(validate func(*LLMSchedulerConfig) error) (*LLMSchedulerConfig, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	config := buildLLMSchedulerConfig()
	if err := validate(config); err != nil {
		return nil, fmt.Errorf("llm scheduler config validation failed: %w", err)
	}
	return config, nil
}

func buildLLMSchedulerConfig() *LLMSchedulerConfig {
	webhookToken, botToken, _, _ := loadRuntimeTokensAndCORS()
	port := sharedenv.Int("LLM_SCHEDULER_PORT", 30003)

	return &LLMSchedulerConfig{
		Server: ServerConfig{
			Port:           port,
			APIKey:         sharedenv.String("API_SECRET_KEY", ""),
			HTTPTransports: parseCommaSeparated(sharedenv.String("HOLOLIVE_HTTP_TRANSPORTS", "h3")),
			H3Addr:         sharedenv.String("HOLOLIVE_H3_ADDR", fmt.Sprintf(":%d", port)),
			H3CertFile:     strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_CERT_FILE", "")),
			H3KeyFile:      strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_KEY_FILE", "")),
			MetricsAddr:    strings.TrimSpace(sharedenv.String("HOLOLIVE_METRICS_ADDR", "")),
		},
		Iris: IrisConfig{
			BaseURL:      sharedenv.String("IRIS_BASE_URL", ""),
			BaseURLFile:  sharedenv.String("IRIS_BASE_URL_FILE", ""),
			WebhookToken: webhookToken,
			BotToken:     botToken,
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Logging:  loadLoggingConfig(),
		Bot: BotConfig{
			Prefix:   sharedenv.String("BOT_PREFIX", "!"),
			SelfUser: sharedenv.String("BOT_SELF_USER", "iris"),
		},
		Environment: loadAppEnvironment(),
		Cliproxy:    loadCliproxyConfig(),
		LLM:         loadLLMConfig(),
		Exa:         loadExaConfig(),
		Version:     sharedenv.String("APP_VERSION", "1.0.0-llm-scheduler"),
	}
}

func (c *LLMSchedulerConfig) validate() error {
	if err := c.validateServerBasics(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Iris.WebhookToken) == "" {
		return fmt.Errorf("IRIS_WEBHOOK_TOKEN is required")
	}
	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return fmt.Errorf("IRIS_BOT_TOKEN is required")
	}
	if strings.TrimSpace(c.Iris.BaseURL) == "" && strings.TrimSpace(c.Iris.BaseURLFile) == "" {
		return fmt.Errorf("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}
	return validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode)
}

func (c *LLMSchedulerConfig) validateRuntime() error {
	if err := c.validateServerBasics(); err != nil {
		return err
	}
	if err := validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	return validateNoNotificationEgressOwnership(runtimeLLMScheduler)
}

func (c *LLMSchedulerConfig) validateServerBasics() error {
	if c.Server.Port == 0 {
		return fmt.Errorf("LLM_SCHEDULER_PORT is required")
	}
	if err := validateServerTransports(&c.Server); err != nil {
		return err
	}
	return validateAPISecretKey(c.Environment, c.Server.APIKey)
}
