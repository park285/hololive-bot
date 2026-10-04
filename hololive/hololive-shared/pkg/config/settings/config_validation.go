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
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/envload"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

func (c *Config) Validate() error {
	if err := c.validateWithRequired(c.validateRequiredConfig); err != nil {
		return fmt.Errorf("validate with required: %w", err)
	}

	return nil
}

// ValidateAdminAPIRuntime: admin-api는 compose 보안 계약상 nonEgress라
// Iris egress 토큰을 받을 수 없으므로 IRIS·YouTube 필수 검증을 면제합니다.
func (c *Config) ValidateAdminAPIRuntime() error {
	if err := c.validateWithRequired(c.validateAdminAPIRequiredConfig); err != nil {
		return fmt.Errorf("validate with required: %w", err)
	}

	if err := runtimepolicy.ValidateNoNotificationEgressOwnership(runtimepolicy.RuntimeAdminAPI, envload.TrimmedEnv(runtimepolicy.NotificationEgressRoleEnv), envload.TrimmedEnv(runtimepolicy.NotificationSchedulerRoleEnv)); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}

func (c *Config) validateWithRequired(validateRequired func() error) error {
	if err := envload.ValidateUnsupportedLegacyEnvUsage(); err != nil {
		return fmt.Errorf("validate unsupported legacy env usage: %w", err)
	}

	if c.Server.Port == 0 {
		return errors.New("SERVER_PORT is required")
	}

	if err := c.validateServerTransports(); err != nil {
		return fmt.Errorf("validate server transports: %w", err)
	}

	if err := runtimepolicy.ValidateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return fmt.Errorf("validate API secret key: %w", err)
	}

	if err := validateRequired(); err != nil {
		return fmt.Errorf("validate required: %w", err)
	}

	if err := runtimepolicy.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return fmt.Errorf("validate postgres SSL mode: %w", err)
	}

	if err := c.validateRuntimeConfigs(); err != nil {
		return fmt.Errorf("validate runtime configs: %w", err)
	}

	return nil
}

func (c *Config) validateRuntimeConfigs() error {
	if err := ValidateTracingConfig(c.Tracing); err != nil {
		return fmt.Errorf("validate tracing config: %w", err)
	}

	if err := validateHolodexConfig(&c.Holodex); err != nil {
		return fmt.Errorf("validate holodex config: %w", err)
	}

	if err := validateOfficialScheduleConfig(&c.OfficialSchedule, c.MaxResponseBodyBytes); err != nil {
		return fmt.Errorf("validate official schedule config: %w", err)
	}

	if err := validateCORSConfig(c.Environment, c.CORS); err != nil {
		return fmt.Errorf("validate CORS config: %w", err)
	}

	return nil
}

func validateHolodexConfig(config *HolodexConfig) error {
	if config == nil {
		return nil
	}

	if err := runtimepolicy.ValidateHolodexTimeout(config.Timeout); err != nil {
		return fmt.Errorf("validate holodex timeout: %w", err)
	}

	if err := ValidateHolodexRequestConfig(config); err != nil {
		return fmt.Errorf("validate holodex request config: %w", err)
	}

	return nil
}

func validateOfficialScheduleConfig(config *OfficialScheduleConfig, maxResponseBodyBytes int64) error {
	if config == nil {
		return errors.New("official schedule config is required")
	}

	if err := runtimepolicy.ValidateOfficialScheduleBaseURL(config.BaseURL); err != nil {
		return fmt.Errorf("validate official schedule base URL: %w", err)
	}

	if err := runtimepolicy.ValidateOfficialScheduleTimeout(config.Timeout); err != nil {
		return fmt.Errorf("validate official schedule timeout: %w", err)
	}

	if config.PageCacheTTL < 0 {
		return errors.New("OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS must be >= 0")
	}

	if maxResponseBodyBytes <= 0 {
		return errors.New("MAX_RESPONSE_BODY_BYTES must be positive")
	}

	return nil
}

func (c *Config) validateAdminAPIRequiredConfig() error {
	if len(c.Kakao.Rooms) == 0 {
		return errors.New("KAKAO_ROOMS is required")
	}

	if err := runtimepolicy.ValidateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	return nil
}

func (c *Config) validateRequiredConfig() error {
	if len(c.Kakao.Rooms) == 0 {
		return errors.New("KAKAO_ROOMS is required")
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

	if err := runtimepolicy.ValidateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	return nil
}

func validateCORSConfig(environment string, config CORSConfig) error {
	if runtimepolicy.IsProduction(environment) && config.Enforce && len(config.AllowedOrigins) == 0 {
		return errors.New("CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true")
	}

	return nil
}
