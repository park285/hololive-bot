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
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
	"github.com/park285/shared-go/pkg/stringutil"
)

func (c *Config) Validate() error {
	return c.validateWithRequired(c.validateRequiredConfig)
}

// ValidateAdminAPIRuntime: admin-api는 compose 보안 계약상 nonEgress라
// Iris egress 토큰을 받을 수 없으므로 IRIS·YouTube 필수 검증을 면제합니다.
func (c *Config) ValidateAdminAPIRuntime() error {
	if err := c.validateWithRequired(c.validateAdminAPIRequiredConfig); err != nil {
		return err
	}
	return validateNoNotificationEgressOwnership(runtimeAdminAPI)
}

func (c *Config) validateWithRequired(validateRequired func() error) error {
	if err := validateUnsupportedLegacyEnvUsage(); err != nil {
		return err
	}
	if c.Server.Port == 0 {
		return fmt.Errorf("SERVER_PORT is required")
	}
	if err := c.validateServerTransports(); err != nil {
		return err
	}
	if err := validateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return err
	}
	if err := validateRequired(); err != nil {
		return err
	}
	if err := validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	return c.validateRuntimeConfigs()
}

func (c *Config) validateRuntimeConfigs() error {
	if err := validateTracingConfig(c.Tracing); err != nil {
		return err
	}
	if err := validateScraperConfig(&c.Scraper); err != nil {
		return err
	}
	if err := validateHolodexConfig(&c.Holodex); err != nil {
		return err
	}
	if err := validateOfficialScheduleConfig(&c.OfficialSchedule, c.MaxResponseBodyBytes); err != nil {
		return err
	}
	if err := validateCORSConfig(c.Environment, c.CORS); err != nil {
		return err
	}
	return nil
}

func validateHolodexConfig(config *HolodexConfig) error {
	if config == nil {
		return nil
	}
	if err := validateHolodexTimeout(config.Timeout); err != nil {
		return err
	}
	fallback := config.LiveStatusFallback
	if fallback.MaxPerCycle <= 0 {
		return fmt.Errorf("HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE must be positive")
	}
	if fallback.WallClockBudget <= 0 {
		return fmt.Errorf("HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS must be positive")
	}
	if fallback.DeadlineMargin < 0 {
		return fmt.Errorf("HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS must be >= 0")
	}
	return nil
}

func validateHolodexTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("HOLODEX_TIMEOUT_SECONDS must be positive")
	}
	return nil
}

func validateHolodexAPIKey(apiKey string) error {
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("HOLODEX_API_KEY is required")
	}
	return nil
}

func validateOfficialScheduleTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("OFFICIAL_SCHEDULE_TIMEOUT_SECONDS must be positive")
	}
	return nil
}

func validateOfficialScheduleConfig(config *OfficialScheduleConfig, maxResponseBodyBytes int64) error {
	if config == nil {
		return fmt.Errorf("official schedule config is required")
	}
	if err := validateOfficialScheduleBaseURL(config.BaseURL); err != nil {
		return err
	}
	if err := validateOfficialScheduleTimeout(config.Timeout); err != nil {
		return err
	}
	if config.CacheExpiry <= 0 {
		return fmt.Errorf("OFFICIAL_SCHEDULE_CACHE_EXPIRY_SECONDS must be positive")
	}
	if config.PageCacheTTL < 0 {
		return fmt.Errorf("OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS must be >= 0")
	}
	if maxResponseBodyBytes <= 0 {
		return fmt.Errorf("MAX_RESPONSE_BODY_BYTES must be positive")
	}
	return nil
}

func validateOfficialScheduleBaseURL(rawURL string) error {
	baseURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("parse OFFICIAL_SCHEDULE_BASE_URL: %w", err)
	}
	if baseURL.Scheme != "https" || baseURL.Host == "" {
		return fmt.Errorf("OFFICIAL_SCHEDULE_BASE_URL must be an HTTPS origin")
	}
	if baseURL.User != nil || (baseURL.Path != "" && baseURL.Path != "/") {
		return fmt.Errorf("OFFICIAL_SCHEDULE_BASE_URL must not contain userinfo or path")
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return fmt.Errorf("OFFICIAL_SCHEDULE_BASE_URL must not contain query or fragment")
	}
	return nil
}

func (c *Config) validateAdminAPIRequiredConfig() error {
	if len(c.Kakao.Rooms) == 0 {
		return fmt.Errorf("KAKAO_ROOMS is required")
	}
	return validateHolodexAPIKey(c.Holodex.APIKey)
}

func (c *Config) validateRequiredConfig() error {
	if len(c.Kakao.Rooms) == 0 {
		return fmt.Errorf("KAKAO_ROOMS is required")
	}
	if strings.TrimSpace(c.Iris.WebhookToken) == "" {
		return fmt.Errorf("IRIS_WEBHOOK_TOKEN is required")
	}
	if !c.Webhook.RequireHMAC {
		return fmt.Errorf("IRIS_WEBHOOK_REQUIRE_HMAC=false is unsupported; Iris webhook HMAC is mandatory")
	}
	if strings.TrimSpace(c.Iris.BotToken) == "" {
		return fmt.Errorf("IRIS_BOT_TOKEN is required")
	}
	if strings.TrimSpace(c.Iris.BaseURL) == "" && strings.TrimSpace(c.Iris.BaseURLFile) == "" {
		return fmt.Errorf("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}
	return validateHolodexAPIKey(c.Holodex.APIKey)
}

func validateScraperConfig(config *ScraperConfig) error {
	if err := validateScraperSchedulerConfig(config.Scheduler); err != nil {
		return err
	}
	if err := validateScraperFetcherEngine(config.FetcherEngine); err != nil {
		return err
	}
	if err := validateScraperBackfillConfig(config.Backfill); err != nil {
		return err
	}
	if err := validateScraperActiveActiveConfig(config.ActiveActive); err != nil {
		return err
	}
	return nil
}

func validateCORSConfig(environment string, config CORSConfig) error {
	if isProductionEnvironment(environment) && config.Enforce && len(config.AllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true")
	}
	return nil
}

func validateScraperSchedulerConfig(config ScraperSchedulerConfig) error {
	if config.PollTimeout == 0 && config.ErrorBackoffMin == 0 && config.ErrorBackoffMax == 0 {
		return nil
	}
	if config.PollTimeout <= 0 {
		return fmt.Errorf("SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS must be positive")
	}
	if config.ErrorBackoffMin <= 0 {
		return fmt.Errorf("SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS must be positive")
	}
	if config.ErrorBackoffMax <= 0 {
		return fmt.Errorf("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be positive")
	}
	if config.ErrorBackoffMax < config.ErrorBackoffMin {
		return fmt.Errorf("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be >= SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS")
	}
	return nil
}

func validateScraperFetcherEngine(engine string) error {
	switch NormalizeScraperFetcherEngine(engine) {
	case ScraperFetcherEngineNetHTTP:
		return nil
	default:
		return fmt.Errorf("SCRAPER_FETCHER_ENGINE must be one of: nethttp (goscrapy has been removed)")
	}
}

func validateScraperActiveActiveConfig(config ScraperActiveActiveConfig) error {
	if !config.Enabled {
		return nil
	}
	if strings.TrimSpace(config.Namespace) == "" {
		return fmt.Errorf("YOUTUBE_PRODUCER_LEASE_NAMESPACE must not be empty when active-active is enabled")
	}
	return nil
}

func validateScraperBackfillConfig(config ScraperBackfillConfig) error {
	if strings.TrimSpace(config.TargetGroup) != "notification" {
		return fmt.Errorf("SCRAPER_BACKFILL_TARGET_GROUP must be notification")
	}
	if !config.Enabled {
		return nil
	}
	if config.ShortsEnabled && config.ShortsInterval <= 0 {
		return fmt.Errorf("SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS must be positive when backfill shorts is enabled")
	}
	if config.LiveEnabled && config.LiveInterval <= 0 {
		return fmt.Errorf("SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS must be positive when backfill live is enabled")
	}
	return nil
}

func loadScraperPoll() ScraperPoll {
	defaults := DefaultScraperPoll()

	return ScraperPoll{
		Videos:    secondsEnv("SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS", defaults.Videos),
		Shorts:    secondsEnv("SCRAPER_POLL_SHORTS_INTERVAL_SECONDS", defaults.Shorts),
		Community: secondsEnv("SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS", defaults.Community),
		Stats:     secondsEnv("SCRAPER_POLL_STATS_INTERVAL_SECONDS", defaults.Stats),
		Live:      secondsEnv("SCRAPER_POLL_LIVE_INTERVAL_SECONDS", defaults.Live),
	}
}

func secondsEnv(key string, fallback time.Duration) time.Duration {
	if seconds := sharedenv.Int(key, 0); seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func validateUnsupportedLegacyEnvUsage() error {
	if value, exists := os.LookupEnv("MEMBER_NEWS_CLIPROXY_MODEL"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("MEMBER_NEWS_CLIPROXY_MODEL is no longer supported; use MEMBER_NEWS_LLM_MODEL")
	}
	if value, exists := os.LookupEnv("DB_SSLMODE"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("DB_SSLMODE is no longer supported; use POSTGRES_SSLMODE")
	}
	if value, exists := os.LookupEnv("DB_QUERY_EXEC_MODE"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("DB_QUERY_EXEC_MODE is no longer supported; use POSTGRES_QUERY_EXEC_MODE")
	}
	if value, exists := os.LookupEnv("OTEL_ENVIRONMENT"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("OTEL_ENVIRONMENT is no longer supported; use APP_ENV")
	}

	return nil
}

func validatePostgresSSLMode(environment, sslMode string) error {
	mode := strings.ToLower(strings.TrimSpace(sslMode))
	if mode == "" {
		return fmt.Errorf("POSTGRES_SSLMODE is required")
	}
	if !isValidPostgresSSLMode(mode) {
		return fmt.Errorf("invalid POSTGRES_SSLMODE: %s", sslMode)
	}
	if !isProductionEnvironment(environment) {
		return nil
	}
	if isInsecurePostgresSSLMode(mode) {
		return fmt.Errorf("POSTGRES_SSLMODE=%s is not allowed in production; use verify-full with POSTGRES_SSLROOTCERT", sslMode)
	}

	return nil
}

func isValidPostgresSSLMode(mode string) bool {
	switch mode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

func isInsecurePostgresSSLMode(mode string) bool {
	switch mode {
	case "disable", "allow", "prefer", "require", "verify-ca":
		return true
	default:
		return false
	}
}

func isProductionEnvironment(environment string) bool {
	return strings.EqualFold(strings.TrimSpace(environment), "production")
}

func validateAPISecretKey(environment, apiKey string) error {
	if !strings.EqualFold(strings.TrimSpace(environment), "production") {
		return nil
	}
	if strings.TrimSpace(apiKey) != "" {
		return nil
	}
	return fmt.Errorf("API_SECRET_KEY is required in production")
}
