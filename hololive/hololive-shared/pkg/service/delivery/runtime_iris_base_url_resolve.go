package delivery

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

func (c *RuntimeIrisClient) ValidateBaseURL() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.resolveBaseURLLocked()
	if err != nil {
		return err
	}
	return nil
}

func (c *RuntimeIrisClient) resolveBaseURLLocked() (string, error) {
	if c == nil {
		return "", fmt.Errorf("runtime iris client: client is nil")
	}

	if c.baseURLFilePath != "" {
		return c.resolveBaseURLFromFileLocked()
	}

	return validateHTTPBaseURL(c.fallbackBaseURL)
}

func (c *RuntimeIrisClient) resolveBaseURLFromFileLocked() (string, error) {
	validateStat := shouldValidateRuntimeIrisBaseURLFileStat()
	baseURLFilePath, err := normalizeRuntimeIrisBaseURLFilePath(c.baseURLFilePath, validateStat)
	if err != nil {
		return "", fmt.Errorf("validate IRIS_BASE_URL_FILE path: %w", err)
	}

	if validateStat {
		if err := validateRuntimeIrisBaseURLFileStat(baseURLFilePath); err != nil {
			return "", fmt.Errorf("validate IRIS_BASE_URL_FILE: %w", err)
		}
	}

	raw, err := os.ReadFile(baseURLFilePath)
	if err != nil {
		return "", fmt.Errorf("read IRIS_BASE_URL_FILE: %w", err)
	}

	baseURL, err := validateRuntimeIrisBaseURLFileOverride(string(raw), c.warnBaseURLHostUnvalidated)
	if err != nil {
		return "", fmt.Errorf("validate IRIS_BASE_URL_FILE URL: %w", err)
	}

	return baseURL, nil
}

func (c *RuntimeIrisClient) warnBaseURLHostUnvalidated(host string) {
	if c == nil || c.logger == nil {
		return
	}

	c.baseURLHostUnvalidatedWarnOnce.Do(func() {
		c.logger.Warn("IRIS_BASE_URL_FILE host is unvalidated because no Iris base URL allowlist is configured",
			slog.String("host", host),
			slog.String("path", c.baseURLFilePath),
			slog.String("allowlist_env", irisH3ServerNameEnv+","+irisBaseURLAllowedHostsEnv),
		)
	})
}

func validateHTTPBaseURL(raw string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if baseURL == "" {
		return "", fmt.Errorf("base URL is empty")
	}

	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return "", err
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme: %q", parsed.Scheme)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("base URL host is empty")
	}

	return baseURL, nil
}
