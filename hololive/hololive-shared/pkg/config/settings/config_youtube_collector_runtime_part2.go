package settings

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

func (c *YouTubeCollectorRuntimeConfig) validateProviders() error {
	if err := validateHolodexAPIKey(c.Holodex.APIKey); err != nil {
		return fmt.Errorf("validate holodex API key: %w", err)
	}

	if err := validateHolodexTimeout(c.Holodex.Transport.Timeout); err != nil {
		return fmt.Errorf("validate holodex timeout: %w", err)
	}

	if err := validateOfficialScheduleBaseURL(c.OfficialSchedule.BaseURL); err != nil {
		return fmt.Errorf("validate official schedule base URL: %w", err)
	}

	if err := validateOfficialScheduleTimeout(c.OfficialSchedule.Transport.Timeout); err != nil {
		return fmt.Errorf("validate official schedule timeout: %w", err)
	}

	return nil
}

func (c *YouTubeCollectorRuntimeConfig) applyCollector() error {
	if err := c.Collector.Validate(c.Holodex.Transport.Timeout, c.OfficialSchedule.Transport.Timeout); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	return nil
}

func applyYouTubeCollectorWorkerProfile(config *YouTubeCollectorRuntimeConfig) {
	profile := config.WorkerProfile
	worker := profile.Loaded.Profile.Workers["collection"]
	settings := profile.Collection

	config.Collector.TotalWorkers = worker.Executor.ConfiguredWorkers
	config.Collector.QueueCapacity = int(*worker.Queue.Capacity.Items)
	config.Collector.AcquisitionBatch = settings.AcquisitionBatch
	config.Collector.AcquisitionCadence = time.Duration(settings.AcquisitionCadenceMS) * time.Millisecond
	config.Collector.LeaseTTL = time.Duration(settings.LeaseTTLMS) * time.Millisecond
	config.Collector.RenewInterval = time.Duration(settings.RenewIntervalMS) * time.Millisecond
	config.Collector.RenewTimeout = time.Duration(settings.RenewTimeoutMS) * time.Millisecond
	config.Collector.DBTimeout = time.Duration(settings.DBTimeoutMS) * time.Millisecond
	config.Collector.CleanupTimeout = time.Duration(settings.CleanupTimeoutMS) * time.Millisecond
	config.Collector.ProviderAdmissionTimeout = time.Duration(settings.ProviderAdmissionTimeoutMS) * time.Millisecond
	config.Collector.CollectionOverhead = time.Duration(settings.CollectionOverheadMS) * time.Millisecond
	config.Collector.PublishTimeout = time.Duration(settings.PublishTimeoutMS) * time.Millisecond
	config.Collector.RetryMin = time.Duration(settings.RetryMinMS) * time.Millisecond
	config.Collector.RetryMax = time.Duration(settings.RetryMaxMS) * time.Millisecond
	config.Collector.ReleaseJitterMin = time.Duration(settings.ReleaseJitterMinMS) * time.Millisecond
	config.Collector.ReleaseJitterMax = time.Duration(settings.ReleaseJitterMaxMS) * time.Millisecond
	config.Collector.HolodexMaxInflight = settings.HolodexMaxInflight
	config.Collector.OfficialMaxInflight = settings.OfficialMaxInflight
	config.Collector.YouTubeJSMaxInflight = settings.YouTubeJSMaxInflight
}

func (c CollectorProxyConfig) Validate() error {
	if !c.Enabled {
		if c.URL == "" {
			return nil
		}

		return fmt.Errorf("SCRAPER_PROXY_URL must be empty when SCRAPER_PROXY_ENABLED=false (got %s)", redactedProxyURL(c.URL))
	}

	if c.URL == "" {
		return errors.New("SCRAPER_PROXY_URL is required when SCRAPER_PROXY_ENABLED=true")
	}

	if err := validateCollectorProxyURL(c.URL); err != nil {
		return fmt.Errorf("validate collector proxy URL: %w", err)
	}

	return nil
}

func validateCollectorProxyURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return errors.New("SCRAPER_PROXY_URL is invalid")
	}

	if err := validateCollectorProxyEndpoint(parsed, raw); err != nil {
		return fmt.Errorf("validate collector proxy endpoint: %w", err)
	}

	if err := validateCollectorProxyResource(parsed, raw); err != nil {
		return fmt.Errorf("validate collector proxy resource: %w", err)
	}

	return nil
}

func validateCollectorProxyEndpoint(parsed *url.URL, raw string) error {
	if parsed.Scheme != schemeHTTP && parsed.Scheme != schemeHTTPS {
		return fmt.Errorf("SCRAPER_PROXY_URL scheme must be http or https (got %s)", redactedProxyURL(raw))
	}

	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("SCRAPER_PROXY_URL host must be non-empty (got %s)", redactedProxyURL(raw))
	}

	return nil
}

func validateCollectorProxyResource(parsed *url.URL, raw string) error {
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("SCRAPER_PROXY_URL path must be empty or / (got %s)", redactedProxyURL(raw))
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("SCRAPER_PROXY_URL must not contain query or fragment (got %s)", redactedProxyURL(raw))
	}

	return nil
}

func redactedProxyURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return "<invalid-proxy-url>"
	}

	if parsed.User != nil {
		parsed.User = url.User("redacted")
	}

	return parsed.String()
}

func validateReadablePostgresSSLRootCert(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("POSTGRES_SSLROOTCERT is required in production")
	}

	file, err := os.Open(trimmed) //nolint:gosec // 운영자가 지정한 CA 경로의 읽기 가능성을 기동 시 검증해야 합니다.
	if err != nil {
		return fmt.Errorf("POSTGRES_SSLROOTCERT is not readable: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("POSTGRES_SSLROOTCERT close: %w", err)
	}

	return nil
}

func resolvedHololiveScraperUser() string {
	user := strings.TrimSpace(sharedenv.String("HOLOLIVE_SCRAPER_USER", postgresScraperRoleUser))
	if user == "" {
		return postgresScraperRoleUser
	}

	return user
}
