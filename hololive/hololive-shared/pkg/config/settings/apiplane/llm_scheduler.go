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

package apiplane

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

type LLMSchedulerConfig struct {
	Server      settings.ServerConfig
	Iris        settings.IrisConfig
	Valkey      settings.ValkeyConfig
	Postgres    settings.PostgresConfig
	Logging     settings.LoggingConfig
	Bot         settings.BotConfig
	Environment string
	LLMProvider string
	Cliproxy    settings.CliproxyConfig
	Gemini      settings.GeminiConfig
	LLM         settings.LLMConfig
	Exa         settings.ExaConfig
	Version     string
}

func LoadLLMScheduler() (*LLMSchedulerConfig, error) {
	out, err := loadLLMSchedulerValidated((*LLMSchedulerConfig).validate)
	if err != nil {
		return nil, fmt.Errorf("load LLM scheduler validated: %w", err)
	}

	return out, nil
}

// LoadLLMSchedulerRuntime: llm-scheduler는 compose 보안 계약상 nonEgress라
// Iris egress 토큰을 받을 수 없으므로 Iris 입력 필수 검증을 면제합니다.
func LoadLLMSchedulerRuntime() (*LLMSchedulerConfig, error) {
	out, err := loadLLMSchedulerValidated((*LLMSchedulerConfig).validateRuntime)
	if err != nil {
		return nil, fmt.Errorf("load LLM scheduler validated: %w", err)
	}

	return out, nil
}

func loadLLMSchedulerValidated(validate func(*LLMSchedulerConfig) error) (*LLMSchedulerConfig, error) {
	if err := load.DotEnv(); err != nil {
		return nil, fmt.Errorf("load dot env: %w", err)
	}

	config := buildLLMSchedulerConfig()
	if err := validate(config); err != nil {
		return nil, fmt.Errorf("llm scheduler config validation failed: %w", err)
	}

	return config, nil
}

func buildLLMSchedulerConfig() *LLMSchedulerConfig {
	webhookToken, botToken, _, _ := settings.LoadRuntimeTokensAndCORS()
	port := sharedenv.Int("LLM_SCHEDULER_PORT", 30003)

	return &LLMSchedulerConfig{
		Server: settings.ServerConfig{
			Port:           port,
			APIKey:         sharedenv.String("API_SECRET_KEY", ""),
			HTTPTransports: load.CommaSeparated(sharedenv.String("HOLOLIVE_HTTP_TRANSPORTS", "h3")),
			H3Addr:         sharedenv.String("HOLOLIVE_H3_ADDR", fmt.Sprintf(":%d", port)),
			H3CertFile:     strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_CERT_FILE", "")),
			H3KeyFile:      strings.TrimSpace(sharedenv.String("HOLOLIVE_H3_KEY_FILE", "")),
			MetricsAddr:    strings.TrimSpace(sharedenv.String("HOLOLIVE_METRICS_ADDR", "")),
		},
		Iris: settings.IrisConfig{
			BaseURL:      sharedenv.String("IRIS_BASE_URL", ""),
			BaseURLFile:  sharedenv.String("IRIS_BASE_URL_FILE", ""),
			WebhookToken: webhookToken,
			BotToken:     botToken,
		},
		Valkey:   settings.LoadValkeyConfig(),
		Postgres: settings.LoadPostgresConfig(),
		Logging:  settings.LoadLoggingConfig(),
		Bot: settings.BotConfig{
			Prefix:   sharedenv.String("BOT_PREFIX", "!"),
			SelfUser: sharedenv.String("BOT_SELF_USER", "iris"),
		},
		Environment: load.AppEnvironment(),
		LLMProvider: strings.ToLower(strings.TrimSpace(sharedenv.String("LLM_PROVIDER", settings.LLMProviderCliproxy))),
		Cliproxy:    settings.LoadCliproxyConfig(),
		Gemini:      settings.LoadGeminiConfig(),
		LLM:         settings.LoadLLMConfig(),
		Exa:         settings.LoadExaConfig(),
		Version:     sharedenv.String("APP_VERSION", "1.0.0-llm-scheduler"),
	}
}

func (c *LLMSchedulerConfig) validate() error {
	if err := c.validateServerBasics(); err != nil {
		return fmt.Errorf("validate server basics: %w", err)
	}

	if strings.TrimSpace(c.Iris.WebhookToken) == "" {
		return errors.New("IRIS_WEBHOOK_TOKEN is required")
	}

	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return errors.New("IRIS_BOT_TOKEN is required")
	}

	if strings.TrimSpace(c.Iris.BaseURL) == "" && strings.TrimSpace(c.Iris.BaseURLFile) == "" {
		return errors.New("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	if err := validateLLMProvider(c.SelectedLLMProvider()); err != nil {
		return fmt.Errorf("validate LLM provider: %w", err)
	}

	if err := load.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	return nil
}

func (c *LLMSchedulerConfig) validateRuntime() error {
	if err := c.validateServerBasics(); err != nil {
		return fmt.Errorf("validate server basics: %w", err)
	}

	if err := load.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := validateLLMProvider(c.SelectedLLMProvider()); err != nil {
		return fmt.Errorf("validate LLM provider: %w", err)
	}

	if err := load.ValidateNoNotificationEgressOwnership(load.RuntimeLLMScheduler); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}

func (c *LLMSchedulerConfig) SelectedLLMProvider() settings.LLMProviderConfig {
	if c == nil {
		return settings.LLMProviderConfig{}
	}

	return settings.LLMProviderConfig{
		Name:     c.LLMProvider,
		Cliproxy: c.Cliproxy,
		Gemini:   c.Gemini,
	}
}

func validateLLMProvider(config settings.LLMProviderConfig) error {
	provider := cmp.Or(strings.ToLower(strings.TrimSpace(config.Name)), settings.LLMProviderCliproxy)

	switch provider {
	case settings.LLMProviderCliproxy:
		if err := validateCliproxyProvider(config.Cliproxy); err != nil {
			return fmt.Errorf("validate cliproxy provider: %w", err)
		}
	case settings.LLMProviderGemini:
		if err := validateGeminiProvider(config.Gemini); err != nil {
			return fmt.Errorf("validate gemini provider: %w", err)
		}
	default:
		return fmt.Errorf("LLM_PROVIDER must be %q or %q", settings.LLMProviderCliproxy, settings.LLMProviderGemini)
	}

	return nil
}

func validateCliproxyProvider(config settings.CliproxyConfig) error {
	if !config.Enabled {
		return nil
	}

	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.Model) == "" {
		return errors.New("selected cliproxy provider configuration is incomplete")
	}

	return nil
}

func validateGeminiProvider(config settings.GeminiConfig) error {
	if !config.Enabled {
		return errors.New("selected gemini provider is disabled")
	}

	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.Model) == "" {
		return errors.New("selected gemini provider configuration is incomplete")
	}

	switch strings.ToLower(strings.TrimSpace(config.ThinkingLevel)) {
	case "low", "medium", "high":
		return nil
	default:
		return errors.New("GEMINI_THINKING_LEVEL must be one of low, medium, high")
	}
}

func (c *LLMSchedulerConfig) validateServerBasics() error {
	if c.Server.Port == 0 {
		return errors.New("LLM_SCHEDULER_PORT is required")
	}

	if err := settings.ValidateServerTransports(&c.Server); err != nil {
		return fmt.Errorf("validate server transports: %w", err)
	}

	if err := load.ValidateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return fmt.Errorf("validate API secret key: %w", err)
	}

	return nil
}
