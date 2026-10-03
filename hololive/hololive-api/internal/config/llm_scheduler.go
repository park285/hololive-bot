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
	"cmp"
	"errors"
	"fmt"
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type LLMSchedulerConfig struct {
	Server                   settings.ServerConfig
	Iris                     settings.IrisConfig
	InternalH3               sharedh3.ClientOptions
	Valkey                   settings.ValkeyConfig
	Postgres                 settings.PostgresConfig
	Logging                  settings.LoggingConfig
	Bot                      settings.BotConfig
	Environment              string
	LLMProvider              string
	Cliproxy                 settings.CliproxyConfig
	Gemini                   settings.GeminiConfig
	LLM                      settings.LLMConfig
	Exa                      settings.ExaConfig
	Version                  string
	MemberNewsXAllowlistPath string
}

// LoadLLMSchedulerRuntime: llm-scheduler는 compose 보안 계약상 nonEgress라
// Iris egress 토큰을 받을 수 없으므로 Iris 입력 필수 검증을 면제합니다.
func LoadLLMSchedulerRuntime() (*LLMSchedulerConfig, error) {
	if err := envload.DotEnv(); err != nil {
		return nil, fmt.Errorf("load dot env: %w", err)
	}

	if err := settings.RejectRetiredLLMEnv(); err != nil {
		return nil, fmt.Errorf("reject retired LLM env: %w", err)
	}

	// DELIVERY_OUTBOX_V3_HANDOFF_MODE를 읽던 llm plane의 delivery module도 이 로더를 거친다.
	if err := settings.RejectRetiredOutboxV3HandoffEnv(); err != nil {
		return nil, fmt.Errorf("reject retired outbox v3 handoff env: %w", err)
	}

	config, err := buildLLMSchedulerConfig()
	if err != nil {
		return nil, fmt.Errorf("build llm scheduler config: %w", err)
	}

	if err := config.validateRuntime(); err != nil {
		return nil, fmt.Errorf("llm scheduler config validation failed: %w", err)
	}

	return config, nil
}

// buildLLMSchedulerConfig는 모든 구획을 읽은 뒤 오류를 합쳐 돌려준다. 오류가 하나라도 있으면 만든 설정은 버린다.
func buildLLMSchedulerConfig() (*LLMSchedulerConfig, error) {
	webhookToken, botToken, _, _ := settings.LoadRuntimeTokensAndCORS()

	port, portErr := sharedenv.IntE("LLM_SCHEDULER_PORT", 30003)
	valkey, valkeyErr := settings.LoadValkeyConfig()
	postgres, postgresErr := settings.LoadPostgresConfig()
	logging, loggingErr := settings.LoadLoggingConfig()
	cliproxy, cliproxyErr := settings.LoadCliproxyConfig()
	gemini, geminiErr := settings.LoadGeminiConfig()
	llm, llmErr := settings.LoadLLMConfig()
	exa, exaErr := settings.LoadExaConfig()
	seeMoreFold, seeMoreFoldErr := settings.LoadSeeMoreFold()

	if err := errors.Join(portErr, valkeyErr, postgresErr, loggingErr, cliproxyErr, geminiErr, llmErr, exaErr, seeMoreFoldErr); err != nil {
		return nil, fmt.Errorf("load llm scheduler env: %w", err)
	}

	return &LLMSchedulerConfig{
		InternalH3: settings.LoadInternalH3ClientOptions(),
		Server: settings.ServerConfig{
			Port:           port,
			APIKey:         sharedenv.String("API_SECRET_KEY", ""),
			HTTPTransports: envload.CommaSeparated(sharedenv.String("HOLOLIVE_HTTP_TRANSPORTS", "h3")),
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
		Valkey:   valkey,
		Postgres: postgres,
		Logging:  logging,
		Bot: settings.BotConfig{
			Prefix:      sharedenv.String("BOT_PREFIX", "!"),
			SelfUser:    sharedenv.String("BOT_SELF_USER", "iris"),
			SeeMoreFold: seeMoreFold,
		},
		Environment:              envload.AppEnvironment(),
		LLMProvider:              strings.ToLower(strings.TrimSpace(sharedenv.String("LLM_PROVIDER", settings.LLMProviderCliproxy))),
		Cliproxy:                 cliproxy,
		Gemini:                   gemini,
		LLM:                      llm,
		Exa:                      exa,
		Version:                  sharedenv.String("APP_VERSION", "1.0.0-llm-scheduler"),
		MemberNewsXAllowlistPath: strings.TrimSpace(sharedenv.StringRaw("MEMBER_NEWS_X_ALLOWLIST_PATH", "")),
	}, nil
}

func (c *LLMSchedulerConfig) validateRuntime() error {
	if err := c.validateServerBasics(); err != nil {
		return fmt.Errorf("validate server basics: %w", err)
	}

	if err := runtimepolicy.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := validateLLMProvider(c.SelectedLLMProvider()); err != nil {
		return fmt.Errorf("validate LLM provider: %w", err)
	}

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeLLMScheduler, envload.TrimmedEnv(runtimepolicy.NotificationEgressRoleEnv), envload.TrimmedEnv(runtimepolicy.NotificationSchedulerRoleEnv)); err != nil {
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

	if err := runtimepolicy.ValidateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return fmt.Errorf("validate API secret key: %w", err)
	}

	return nil
}
