package delivery

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
)

type runtimeIrisBaseURLResolver struct {
	fallbackBaseURL string
	baseURLFilePath string
	logger          *slog.Logger
	warnOnce        sync.Once
}

func (r *runtimeIrisBaseURLResolver) resolve() (string, error) {
	if r.baseURLFilePath != "" {
		return r.resolveFromFile()
	}

	return validateHTTPBaseURL(r.fallbackBaseURL)
}

func (r *runtimeIrisBaseURLResolver) resolveFromFile() (string, error) {
	validateStat := shouldValidateRuntimeIrisBaseURLFileStat()
	baseURLFilePath, err := normalizeRuntimeIrisBaseURLFilePath(r.baseURLFilePath, validateStat)
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

	baseURL, err := validateRuntimeIrisBaseURLFileOverride(string(raw), r.warnBaseURLHostUnvalidated)
	if err != nil {
		return "", fmt.Errorf("validate IRIS_BASE_URL_FILE URL: %w", err)
	}

	return baseURL, nil
}

func (r *runtimeIrisBaseURLResolver) warnBaseURLHostUnvalidated(host string) {
	if r.logger == nil {
		return
	}

	r.warnOnce.Do(func() {
		r.logger.Warn("IRIS_BASE_URL_FILE host is unvalidated because no Iris base URL allowlist is configured",
			slog.String("host", host),
			slog.String("path", r.baseURLFilePath),
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
