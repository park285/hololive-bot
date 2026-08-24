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

	"github.com/park285/shared-go/v2/pkg/stringutil"
)

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
		value, err := requiredSecondsDurationEnv(field.key, field.fallback)
		if err != nil {
			return ScraperPoll{}, fmt.Errorf("required seconds duration env: %w", err)
		}

		*field.target = value
	}

	return poll, nil
}

func validateUnsupportedLegacyEnvUsage() error {
	if value, exists := os.LookupEnv("MEMBER_NEWS_CLIPROXY_MODEL"); exists && stringutil.TrimSpace(value) != "" {
		return errors.New("MEMBER_NEWS_CLIPROXY_MODEL is no longer supported; use MEMBER_NEWS_LLM_MODEL")
	}

	if value, exists := os.LookupEnv("DB_SSLMODE"); exists && stringutil.TrimSpace(value) != "" {
		return errors.New("DB_SSLMODE is no longer supported; use POSTGRES_SSLMODE")
	}

	if value, exists := os.LookupEnv("DB_QUERY_EXEC_MODE"); exists && stringutil.TrimSpace(value) != "" {
		return errors.New("DB_QUERY_EXEC_MODE is no longer supported; use POSTGRES_QUERY_EXEC_MODE")
	}

	if value, exists := os.LookupEnv("OTEL_ENVIRONMENT"); exists && stringutil.TrimSpace(value) != "" {
		return errors.New("OTEL_ENVIRONMENT is no longer supported; use APP_ENV")
	}

	return nil
}

func validatePostgresSSLMode(environment, sslMode string) error {
	mode := strings.ToLower(strings.TrimSpace(sslMode))
	if mode == "" {
		return errors.New("POSTGRES_SSLMODE is required")
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
	return strings.EqualFold(strings.TrimSpace(environment), environmentProduction)
}

func validateAPISecretKey(environment, apiKey string) error {
	if !strings.EqualFold(strings.TrimSpace(environment), environmentProduction) {
		return nil
	}

	if strings.TrimSpace(apiKey) != "" {
		return nil
	}

	return errors.New("API_SECRET_KEY is required in production")
}
