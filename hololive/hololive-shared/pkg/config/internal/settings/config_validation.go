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
	"time"

	sharedenv "github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func (c *Config) Validate() error {
	if err := validateDeprecatedEnvUsage(); err != nil {
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
	if err := c.validateRequiredConfig(); err != nil {
		return err
	}
	if err := validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	if err := validateScraperConfig(c.Scraper); err != nil {
		return err
	}
	if err := validateCORSConfig(c.Environment, c.CORS); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateRequiredConfig() error {
	if len(c.Kakao.Rooms) == 0 {
		return fmt.Errorf("KAKAO_ROOMS is required")
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
	if strings.TrimSpace(c.Holodex.APIKey) == "" {
		return fmt.Errorf("HOLODEX_API_KEY is required")
	}
	if isPlaceholderAPIKey(c.YouTube.APIKey) {
		return fmt.Errorf("YOUTUBE_API_KEY uses placeholder value; set a real API key")
	}
	return nil
}

func validateScraperConfig(cfg ScraperConfig) error {
	if err := validateScraperSchedulerConfig(cfg.Scheduler); err != nil {
		return err
	}
	if err := validateScraperFetcherEngine(cfg.FetcherEngine); err != nil {
		return err
	}
	if err := validateScraperPublishedAtResolverConfig(cfg.PublishedAtResolver); err != nil {
		return err
	}
	if err := validateScraperBackfillConfig(cfg.Backfill); err != nil {
		return err
	}
	if err := validateScraperActiveActiveConfig(cfg.ActiveActive); err != nil {
		return err
	}
	return nil
}

func validateCORSConfig(environment string, cfg CORSConfig) error {
	if isProductionEnvironment(environment) && cfg.Enforce && len(cfg.AllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS is required in production when CORS_ENFORCE=true")
	}
	return nil
}

func validateScraperSchedulerConfig(cfg ScraperSchedulerConfig) error {
	if cfg.PollTimeout == 0 && cfg.ErrorBackoffMin == 0 && cfg.ErrorBackoffMax == 0 {
		return nil
	}
	if cfg.PollTimeout <= 0 {
		return fmt.Errorf("SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS must be positive")
	}
	if cfg.ErrorBackoffMin <= 0 {
		return fmt.Errorf("SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS must be positive")
	}
	if cfg.ErrorBackoffMax <= 0 {
		return fmt.Errorf("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be positive")
	}
	if cfg.ErrorBackoffMax < cfg.ErrorBackoffMin {
		return fmt.Errorf("SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS must be >= SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS")
	}
	return nil
}

func validateScraperFetcherEngine(engine string) error {
	switch NormalizeScraperFetcherEngine(engine) {
	case ScraperFetcherEngineNetHTTP, ScraperFetcherEngineGoScrapy:
		return nil
	default:
		return fmt.Errorf("SCRAPER_FETCHER_ENGINE must be one of: nethttp, goscrapy")
	}
}

func validateScraperPublishedAtResolverConfig(cfg ScraperPublishedAtResolverConfig) error {
	if !cfg.Enabled {
		return nil
	}

	checks := []struct {
		valid   bool
		message string
	}{
		{cfg.Interval > 0, "SCRAPER_PUBLISHED_AT_RESOLVER_INTERVAL_SECONDS must be positive when resolver is enabled"},
		{cfg.BatchSize > 0, "SCRAPER_PUBLISHED_AT_RESOLVER_BATCH_SIZE must be positive when resolver is enabled"},
		{cfg.MaxResolvePerRun > 0, "SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RESOLVE_PER_RUN must be positive when resolver is enabled"},
		{cfg.MaxRunDuration > 0, "SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS must be positive when resolver is enabled"},
		{cfg.ResolveTimeout > 0, "SCRAPER_PUBLISHED_AT_RESOLVER_RESOLVE_TIMEOUT_SECONDS must be positive when resolver is enabled"},
	}
	for _, check := range checks {
		if !check.valid {
			return errors.New(check.message)
		}
	}
	if cfg.MaxRunDuration < cfg.ResolveTimeout {
		return fmt.Errorf("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS must be >= SCRAPER_PUBLISHED_AT_RESOLVER_RESOLVE_TIMEOUT_SECONDS")
	}
	tailChecks := []struct {
		valid   bool
		message string
	}{
		{cfg.MinDetectedAge > 0, "SCRAPER_PUBLISHED_AT_RESOLVER_MIN_DETECTED_AGE_SECONDS must be positive when resolver is enabled"},
		{cfg.FailureBackoffTTL > 0, "SCRAPER_PUBLISHED_AT_RESOLVER_FAILURE_BACKOFF_SECONDS must be positive when resolver is enabled"},
	}
	for _, check := range tailChecks {
		if !check.valid {
			return errors.New(check.message)
		}
	}
	return nil
}

func validateScraperActiveActiveConfig(cfg ScraperActiveActiveConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Namespace) == "" {
		return fmt.Errorf("YOUTUBE_PRODUCER_LEASE_NAMESPACE must not be empty when active-active is enabled")
	}
	return nil
}

func validateScraperBackfillConfig(cfg ScraperBackfillConfig) error {
	if strings.TrimSpace(cfg.TargetGroup) != "notification" {
		return fmt.Errorf("SCRAPER_BACKFILL_TARGET_GROUP must be notification")
	}
	if !cfg.Enabled {
		return nil
	}
	if cfg.ShortsEnabled && cfg.ShortsInterval <= 0 {
		return fmt.Errorf("SCRAPER_BACKFILL_SHORTS_INTERVAL_SECONDS must be positive when backfill shorts is enabled")
	}
	if cfg.CommunityEnabled && cfg.CommunityInterval <= 0 {
		return fmt.Errorf("SCRAPER_BACKFILL_COMMUNITY_INTERVAL_SECONDS must be positive when backfill community is enabled")
	}
	if cfg.LiveEnabled && cfg.LiveInterval <= 0 {
		return fmt.Errorf("SCRAPER_BACKFILL_LIVE_INTERVAL_SECONDS must be positive when backfill live is enabled")
	}
	return nil
}

func loadScraperPoll() ScraperPoll {
	defaults := DefaultScraperPoll()

	return ScraperPoll{
		Videos: secondsAliasEnv([]string{
			"SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS",
			"SCRAPER_VIDEOS_SECONDS",
		}, defaults.Videos),
		Shorts: secondsAliasEnv([]string{
			"SCRAPER_POLL_SHORTS_INTERVAL_SECONDS",
			"SCRAPER_SHORTS_SECONDS",
		}, defaults.Shorts),
		Community: secondsAliasEnv([]string{
			"SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS",
			"SCRAPER_COMMUNITY_SECONDS",
		}, defaults.Community),
		Stats: secondsAliasEnv([]string{
			"SCRAPER_POLL_STATS_INTERVAL_SECONDS",
			"SCRAPER_STATS_SECONDS",
		}, defaults.Stats),
		Live: secondsAliasEnv([]string{
			"SCRAPER_POLL_LIVE_INTERVAL_SECONDS",
			"SCRAPER_LIVE_SECONDS",
		}, defaults.Live),
	}
}

func secondsAliasEnv(keys []string, fallback time.Duration) time.Duration {
	for _, key := range keys {
		seconds := sharedenv.Int(key, 0)
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}

func intAliasEnv(keys []string, fallback int) int {
	for _, key := range keys {
		value := sharedenv.Int(key, 0)
		if value > 0 {
			return value
		}
	}
	return fallback
}

func isPlaceholderAPIKey(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "your_api_key", "your_youtube_api_key", "changeme", "change_me", "replace_me", "replace-with-real-key":
		return true
	default:
		return false
	}
}

func validateDeprecatedEnvUsage() error {
	if value, exists := os.LookupEnv("MEMBER_NEWS_CLIPROXY_MODEL"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("MEMBER_NEWS_CLIPROXY_MODEL is no longer supported; use MEMBER_NEWS_LLM_MODEL")
	}
	if value, exists := os.LookupEnv("DB_SSLMODE"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("DB_SSLMODE is no longer supported; use POSTGRES_SSLMODE")
	}
	if value, exists := os.LookupEnv("DB_QUERY_EXEC_MODE"); exists && stringutil.TrimSpace(value) != "" {
		return fmt.Errorf("DB_QUERY_EXEC_MODE is no longer supported; use POSTGRES_QUERY_EXEC_MODE")
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
	if sharedenv.Bool("POSTGRES_SSLMODE_ALLOW_INSECURE", false) {
		return nil
	}
	if isInsecurePostgresSSLMode(mode) {
		return fmt.Errorf("POSTGRES_SSLMODE=%s is not allowed in production; use require, verify-ca, or verify-full", sslMode)
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
	case "disable", "allow", "prefer":
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
