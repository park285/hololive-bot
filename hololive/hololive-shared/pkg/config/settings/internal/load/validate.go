package load

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
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

// 퇴역한 env 이름과 대체 이름이다. 값을 읽지 않고 존재만으로(빈 값 포함) 거절해 퇴역 키가 남은 호스트를 드러낸다.
// 가드 도입 리비전: 9e8f27b03(MEMBER_NEWS_CLIPROXY_MODEL·DB_SSLMODE·DB_QUERY_EXEC_MODE), f1b6ef350(OTEL_ENVIRONMENT).
// 존재 기준 전환: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T17). 그 전에는 빈 값을 통과시켰다.
//
// 배포 선행 조건이자 제거 조건: 이 가드를 부르는 runtime(hololive-api·alarm-worker의 settings.LoadConfig, youtube-collector의
// RuntimeConfig.Validate)의 env 원천인 중앙 compose.env·bot.env·alarm-worker.env, AP ap-compose.env, 모든
// youtube-collector.env(HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE)와 host-native collector env, 그 stack-secrets master 사본,
// 실행 중 프로세스 env에 아래 네 키가 0건이어야 한다. T18(2026-09-26)에서 모든 hololive env에 0건임을 확인했다.
// 이 존재 기준 가드가 든 release가 중앙과 모든 collector 호스트에 배포된 뒤 이 표와 함수, 두 호출(config_validation.go,
// collector/runtime.go), 거절 테스트를 함께 삭제한다.
// 재검토 기한: remove_after = "2026-12-31".
var retiredLegacyEnvKeys = []struct {
	key         string
	replacement string
}{
	{key: "MEMBER_NEWS_CLIPROXY_MODEL", replacement: "MEMBER_NEWS_LLM_MODEL"},
	{key: "DB_SSLMODE", replacement: "POSTGRES_SSLMODE"},
	{key: "DB_QUERY_EXEC_MODE", replacement: "POSTGRES_QUERY_EXEC_MODE"},
	{key: "OTEL_ENVIRONMENT", replacement: "APP_ENV"},
}

// ValidateUnsupportedLegacyEnvUsage: 퇴역한 환경변수가 빈 값으로라도 남아 있으면 기동을 막는다.
func ValidateUnsupportedLegacyEnvUsage() error {
	for _, retired := range retiredLegacyEnvKeys {
		if _, exists := os.LookupEnv(retired.key); exists {
			return fmt.Errorf("%s is retired; use %s and remove it from the runtime env", retired.key, retired.replacement)
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
