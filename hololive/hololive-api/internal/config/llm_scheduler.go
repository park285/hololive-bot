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
	"errors"
	"fmt"
	"strings"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// LLMSchedulerConfig는 llm plane(정기 알림 scheduler·member news·major event)이 소비하는 설정만 담는다.
// LLM plane은 compose 보안 계약상 nonEgress라 Iris egress 입력을 갖지 않는다.
type LLMSchedulerConfig struct {
	Server                   settings.ServerConfig
	Postgres                 settings.PostgresConfig
	Bot                      settings.BotConfig
	Environment              string
	LLMProvider              string
	Cliproxy                 CliproxyConfig
	Gemini                   GeminiConfig
	LLM                      LLMConfig
	Exa                      ExaConfig
	MemberNewsXAllowlistPath string
}

// buildLLMSchedulerConfig는 모든 구획을 읽은 뒤 오류를 합쳐 돌려준다. 오류가 하나라도 있으면 만든 설정은 버린다.
// Listener·pool 값은 configurePlanes가 채우고, 역할 검증은 RuntimeConfig.Validate가 한 번 한다.
func buildLLMSchedulerConfig() (*LLMSchedulerConfig, error) {
	postgres, postgresErr := settings.LoadPostgresEndpointConfig()
	cliproxy, cliproxyErr := LoadCliproxyConfig()
	gemini, geminiErr := LoadGeminiConfig()
	llm, llmErr := LoadLLMConfig()
	exa, exaErr := LoadExaConfig()
	seeMoreFold, seeMoreFoldErr := settings.LoadSeeMoreFold()

	if err := errors.Join(postgresErr, cliproxyErr, geminiErr, llmErr, exaErr, seeMoreFoldErr); err != nil {
		return nil, fmt.Errorf("load llm scheduler env: %w", err)
	}

	return &LLMSchedulerConfig{
		Server:   settings.ServerConfig{APIKey: sharedenv.String("API_SECRET_KEY", "")},
		Postgres: postgres,
		Bot: settings.BotConfig{
			Prefix:      sharedenv.String("BOT_PREFIX", "!"),
			SelfUser:    sharedenv.String("BOT_SELF_USER", "iris"),
			SeeMoreFold: seeMoreFold,
		},
		Environment:              envload.AppEnvironment(),
		LLMProvider:              strings.ToLower(strings.TrimSpace(sharedenv.String("LLM_PROVIDER", LLMProviderCliproxy))),
		Cliproxy:                 cliproxy,
		Gemini:                   gemini,
		LLM:                      llm,
		Exa:                      exa,
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

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeLLMScheduler, sharedenv.String(runtimepolicy.NotificationEgressRoleEnv, ""), sharedenv.String(runtimepolicy.NotificationSchedulerRoleEnv, "")); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}

func (c *LLMSchedulerConfig) SelectedLLMProvider() LLMProviderConfig {
	if c == nil {
		return LLMProviderConfig{}
	}

	return LLMProviderConfig{
		Name:     c.LLMProvider,
		Cliproxy: c.Cliproxy,
		Gemini:   c.Gemini,
	}
}

// validateLLMProvider는 buildLLMSchedulerConfig가 LLM_PROVIDER 기본값과 소문자화를 적용한 이름만 받는다.
// 이름 정규화는 그 적재 단계 한 곳에서만 하므로 빈 이름도 거절한다.
func validateLLMProvider(config LLMProviderConfig) error {
	switch config.Name {
	case LLMProviderCliproxy:
		if err := validateCliproxyProvider(config.Cliproxy); err != nil {
			return fmt.Errorf("validate cliproxy provider: %w", err)
		}
	case LLMProviderGemini:
		if err := validateGeminiProvider(config.Gemini); err != nil {
			return fmt.Errorf("validate gemini provider: %w", err)
		}
	default:
		return fmt.Errorf("LLM_PROVIDER must be %q or %q", LLMProviderCliproxy, LLMProviderGemini)
	}

	return nil
}

func validateCliproxyProvider(config CliproxyConfig) error {
	if !config.Enabled {
		return nil
	}

	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.Model) == "" {
		return errors.New("selected cliproxy provider configuration is incomplete")
	}

	return nil
}

func validateGeminiProvider(config GeminiConfig) error {
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
