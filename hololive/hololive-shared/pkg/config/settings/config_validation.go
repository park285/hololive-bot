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
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
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

	if err := load.ValidateNoNotificationEgressOwnership(load.RuntimeAdminAPI); err != nil {
		return fmt.Errorf("validate no notification egress ownership: %w", err)
	}

	return nil
}

func (c *Config) validateWithRequired(validateRequired func() error) error {
	if err := load.ValidateUnsupportedLegacyEnvUsage(); err != nil {
		return fmt.Errorf("validate unsupported legacy env usage: %w", err)
	}

	if c.Server.Port == 0 {
		return errors.New("SERVER_PORT is required")
	}

	if err := c.validateServerTransports(); err != nil {
		return fmt.Errorf("validate server transports: %w", err)
	}

	if err := load.ValidateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return fmt.Errorf("validate API secret key: %w", err)
	}

	if err := validateRequired(); err != nil {
		return fmt.Errorf("validate required: %w", err)
	}

	if err := load.ValidatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
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

	if err := validateScraperConfig(&c.Scraper); err != nil {
		return fmt.Errorf("validate scraper config: %w", err)
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

	if err := load.ValidateHolodexTimeout(config.Timeout); err != nil {
		return fmt.Errorf("validate holodex timeout: %w", err)
	}

	fallback := config.LiveStatusFallback
	if fallback.MaxPerCycle <= 0 {
		return errors.New("HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE must be positive")
	}

	if fallback.WallClockBudget <= 0 {
		return errors.New("HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS must be positive")
	}

	if fallback.DeadlineMargin < 0 {
		return errors.New("HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS must be >= 0")
	}

	return nil
}

func validateOfficialScheduleConfig(config *OfficialScheduleConfig, maxResponseBodyBytes int64) error {
	if config == nil {
		return errors.New("official schedule config is required")
	}

	if err := load.ValidateOfficialScheduleBaseURL(config.BaseURL); err != nil {
		return fmt.Errorf("validate official schedule base URL: %w", err)
	}

	if err := load.ValidateOfficialScheduleTimeout(config.Timeout); err != nil {
		return fmt.Errorf("validate official schedule timeout: %w", err)
	}

	if config.CacheExpiry <= 0 {
		return errors.New("OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS must be positive")
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

	if err := load.ValidateHolodexAPIKey(c.Holodex.APIKey); err != nil {
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

	if !c.Webhook.RequireHMAC {
		return errors.New("IRIS_WEBHOOK_REQUIRE_HMAC=false is unsupported; Iris webhook HMAC is mandatory")
	}

	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return errors.New("IRIS_BOT_TOKEN is required")
	}

	if strings.TrimSpace(c.Iris.BaseURL) == "" && strings.TrimSpace(c.Iris.BaseURLFile) == "" {
		return errors.New("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	if err := load.ValidateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	return nil
}

func validateScraperConfig(config *ScraperConfig) error {
	if err := validateScraperSchedulerConfig(config.Scheduler); err != nil {
		return fmt.Errorf("validate scraper scheduler config: %w", err)
	}

	if err := validateScraperFetcherEngine(config.FetcherEngine); err != nil {
		return fmt.Errorf("validate scraper fetcher engine: %w", err)
	}

	if err := validateScraperBackfillConfig(config.Backfill); err != nil {
		return fmt.Errorf("validate scraper backfill config: %w", err)
	}

	if err := validateScraperActiveActiveConfig(config.ActiveActive); err != nil {
		return fmt.Errorf("validate scraper active active config: %w", err)
	}

	return nil
}

func validateCORSConfig(environment string, config CORSConfig) error {
	if load.IsProduction(environment) && config.Enforce && len(config.AllowedOrigins) == 0 {
		return errors.New("CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true")
	}

	return nil
}

func validateScraperSchedulerConfig(config ScraperSchedulerConfig) error {
	if config.PollTimeout == 0 && config.ErrorBackoffMin == 0 && config.ErrorBackoffMax == 0 {
		return nil
	}

	if config.PollTimeout <= 0 {
		return errors.New("SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS must be positive")
	}

	if config.ErrorBackoffMin <= 0 {
		return errors.New("SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS must be positive")
	}

	if config.ErrorBackoffMax <= 0 {
		return errors.New("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be positive")
	}

	if config.ErrorBackoffMax < config.ErrorBackoffMin {
		return errors.New("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be >= SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS")
	}

	return nil
}

func validateScraperFetcherEngine(engine string) error {
	switch NormalizeScraperFetcherEngine(engine) {
	case ScraperFetcherEngineNetHTTP:
		return nil
	default:
		return errors.New("SCRAPER_FETCHER_ENGINE must be one of: nethttp (goscrapy has been removed)")
	}
}

func validateScraperActiveActiveConfig(config ScraperActiveActiveConfig) error {
	if !config.Enabled {
		return nil
	}

	if strings.TrimSpace(config.Namespace) == "" {
		return errors.New("YOUTUBE_PRODUCER_LEASE_NAMESPACE must not be empty when active-active is enabled")
	}

	return nil
}

func validateScraperBackfillConfig(config ScraperBackfillConfig) error {
	if strings.TrimSpace(config.TargetGroup) != "notification" {
		return errors.New("SCRAPER_BACKFILL_TARGET_GROUP must be notification")
	}

	if !config.Enabled {
		return nil
	}

	if config.ShortsEnabled && config.ShortsInterval <= 0 {
		return errors.New("SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS must be positive when backfill shorts is enabled")
	}

	if config.LiveEnabled && config.LiveInterval <= 0 {
		return errors.New("SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS must be positive when backfill live is enabled")
	}

	return nil
}

func loadScraperPoll() (ScraperPoll, error) {
	defaults := DefaultScraperPoll()

	var poll ScraperPoll

	for _, field := range []struct {
		key      string
		fallback time.Duration
		target   *time.Duration
	}{
		{key: "SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS", fallback: defaults.Videos, target: &poll.Videos},
		{key: "SCRAPER_POLL_SHORTS_INTERVAL_SECONDS", fallback: defaults.Shorts, target: &poll.Shorts},
		{key: "SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS", fallback: defaults.Community, target: &poll.Community},
		{key: "SCRAPER_POLL_STATS_INTERVAL_SECONDS", fallback: defaults.Stats, target: &poll.Stats},
		{key: "SCRAPER_POLL_LIVE_INTERVAL_SECONDS", fallback: defaults.Live, target: &poll.Live},
	} {
		value, err := load.RequiredSecondsDurationEnv(field.key, field.fallback)
		if err != nil {
			return ScraperPoll{}, fmt.Errorf("required seconds duration env: %w", err)
		}

		*field.target = value
	}

	return poll, nil
}
