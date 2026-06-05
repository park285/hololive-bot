package settings

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
)

const (
	settingsIrisBaseURLAllowedHostsEnv = "IRIS_BASE_URL_ALLOWED_HOSTS"
	settingsIrisH3ServerNameEnv        = "IRIS_H3_SERVER_NAME"
)

var settingsIrisBaseURLUnvalidatedHostWarnOnce sync.Once

func resolveIrisBaseURL(config IrisConfig) (string, error) {
	if baseURL := strings.TrimSpace(config.BaseURL); baseURL != "" {
		return validateSettingsIrisBaseURL(baseURL, "IRIS_BASE_URL")
	}
	return resolveIrisBaseURLFile(config.BaseURLFile)
}

func resolveIrisBaseURLFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	raw, err := os.ReadFile(path) //nolint:gosec // Runtime operator-provided Iris base URL file.
	if err != nil {
		return "", fmt.Errorf("read IRIS_BASE_URL_FILE: %w", err)
	}

	baseURL := strings.TrimSpace(string(raw))
	if baseURL == "" {
		return "", fmt.Errorf("IRIS_BASE_URL_FILE is empty")
	}
	return validateSettingsIrisBaseURL(baseURL, "IRIS_BASE_URL_FILE")
}

func validateSettingsIrisBaseURL(raw, source string) (string, error) {
	baseURL := strings.TrimSpace(raw)
	if baseURL == "" {
		return "", fmt.Errorf("%s is empty", source)
	}

	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return "", err
	}
	return validateParsedSettingsIrisBaseURL(baseURL, source, parsed)
}

func validateParsedSettingsIrisBaseURL(baseURL, source string, parsed *url.URL) (string, error) {
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("%s requires https URL, got %s", source, parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%s URL host is empty", source)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%s URL must not include userinfo", source)
	}
	if err := validateSettingsIrisBaseURLHost(source, parsed.Hostname()); err != nil {
		return "", err
	}
	if parsed.Path == "/" {
		return strings.TrimSuffix(baseURL, "/"), nil
	}
	return baseURL, nil
}

func validateSettingsIrisBaseURLHost(source, host string) error {
	normalizedHost := normalizeSettingsIrisBaseURLHost(host)
	if normalizedHost == "" {
		return fmt.Errorf("%s URL host is empty", source)
	}

	allowedHosts := settingsIrisAllowedBaseURLHosts()
	if _, ok := allowedHosts[normalizedHost]; ok {
		return nil
	}
	if settingsIrisBaseURLHostAllowlistConfigured() {
		return fmt.Errorf("%s host %q must match %s or %s", source, host, settingsIrisH3ServerNameEnv, settingsIrisBaseURLAllowedHostsEnv)
	}
	warnSettingsIrisBaseURLHostUnvalidated(source, host)
	return nil
}

func warnSettingsIrisBaseURLHostUnvalidated(source, host string) {
	settingsIrisBaseURLUnvalidatedHostWarnOnce.Do(func() {
		slog.Warn(source+" host is unvalidated because no Iris base URL allowlist is configured",
			slog.String("host", host),
			slog.String("allowlist_env", settingsIrisH3ServerNameEnv+","+settingsIrisBaseURLAllowedHostsEnv),
		)
	})
}

func settingsIrisBaseURLHostAllowlistConfigured() bool {
	return strings.TrimSpace(os.Getenv(settingsIrisH3ServerNameEnv)) != "" ||
		strings.TrimSpace(os.Getenv(settingsIrisBaseURLAllowedHostsEnv)) != ""
}

func settingsIrisAllowedBaseURLHosts() map[string]struct{} {
	allowedHosts := make(map[string]struct{})
	for _, rawHost := range append(
		[]string{os.Getenv(settingsIrisH3ServerNameEnv)},
		strings.Split(os.Getenv(settingsIrisBaseURLAllowedHostsEnv), ",")...,
	) {
		host := normalizeSettingsIrisBaseURLHost(rawHost)
		if host == "" {
			continue
		}
		allowedHosts[host] = struct{}{}
	}
	return allowedHosts
}

func normalizeSettingsIrisBaseURLHost(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" {
		return ""
	}
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "."), "[")
	host = strings.TrimSuffix(host, "]")
	return host
}
