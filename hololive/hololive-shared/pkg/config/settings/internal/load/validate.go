package load

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"
)

func ValidateAPISecretKey(environment, apiKey string) error {
	if !IsProduction(environment) {
		return nil
	}

	if strings.TrimSpace(apiKey) != "" {
		return nil
	}

	return errors.New("API_SECRET_KEY is required in production")
}

func ValidatePostgresSSLMode(environment, sslMode string) error {
	mode := strings.ToLower(strings.TrimSpace(sslMode))
	if mode == "" {
		return errors.New("POSTGRES_SSLMODE is required")
	}

	if !isValidPostgresSSLMode(mode) {
		return fmt.Errorf("invalid POSTGRES_SSLMODE: %s", sslMode)
	}

	if !IsProduction(environment) {
		return nil
	}

	if isInsecurePostgresSSLMode(mode) {
		return fmt.Errorf("POSTGRES_SSLMODE=%s is not allowed in production; use verify-full with POSTGRES_SSLROOTCERT", sslMode)
	}

	return nil
}

func isValidPostgresSSLMode(mode string) bool {
	switch mode {
	case "disable", "allow", "prefer", "require", "verify-ca", PostgresSSLModeVerifyFull:
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

// ValidateUnsupportedLegacyEnvUsage: 퇴역한 환경변수가 남아 있으면 기동을 막는다.
func ValidateUnsupportedLegacyEnvUsage() error {
	for _, retired := range []struct {
		key         string
		replacement string
	}{
		{key: "MEMBER_NEWS_CLIPROXY_MODEL", replacement: "MEMBER_NEWS_LLM_MODEL"},
		{key: "DB_SSLMODE", replacement: "POSTGRES_SSLMODE"},
		{key: "DB_QUERY_EXEC_MODE", replacement: "POSTGRES_QUERY_EXEC_MODE"},
		{key: "OTEL_ENVIRONMENT", replacement: "APP_ENV"},
	} {
		if value, exists := os.LookupEnv(retired.key); exists && stringutil.TrimSpace(value) != "" {
			return fmt.Errorf("%s is no longer supported; use %s", retired.key, retired.replacement)
		}
	}

	return nil
}

func ValidateHolodexAPIKey(apiKey string) error {
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("HOLODEX_API_KEY is required")
	}

	return nil
}

func ValidateHolodexTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("HOLODEX_TIMEOUT_SECONDS must be positive")
	}

	return nil
}

func ValidateOfficialScheduleTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("OFFICIAL_SCHEDULE_TIMEOUT_SECONDS must be positive")
	}

	return nil
}

func ValidateOfficialScheduleBaseURL(rawURL string) error {
	baseURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("parse OFFICIAL_SCHEDULE_BASE_URL: %w", err)
	}

	if baseURL.Scheme != SchemeHTTPS || baseURL.Host == "" {
		return errors.New("OFFICIAL_SCHEDULE_BASE_URL must be an HTTPS origin")
	}

	if baseURL.User != nil || (baseURL.Path != "" && baseURL.Path != "/") {
		return errors.New("OFFICIAL_SCHEDULE_BASE_URL must not contain userinfo or path")
	}

	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return errors.New("OFFICIAL_SCHEDULE_BASE_URL must not contain query or fragment")
	}

	return nil
}
