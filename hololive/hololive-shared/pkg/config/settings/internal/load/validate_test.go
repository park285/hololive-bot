package load

import (
	"strings"
	"testing"
)

func TestIsValidPostgresSSLMode(t *testing.T) {
	t.Parallel()

	valid := []string{"disable", "allow", "prefer", "require", "verify-ca", PostgresSSLModeVerifyFull}
	for _, mode := range valid {
		if !isValidPostgresSSLMode(mode) {
			t.Fatalf("isValidPostgresSSLMode(%q) = false, want true", mode)
		}
	}

	if isValidPostgresSSLMode("invalid") {
		t.Fatal("isValidPostgresSSLMode(\"invalid\") = true, want false")
	}
}

// 퇴역 가드는 존재 기준이다. 키가 빈 값으로 남아 있어도 퇴역 키가 남은 호스트를 드러내야 한다.
func TestValidateUnsupportedLegacyEnvUsageRejectsPresentEmptyKey(t *testing.T) {
	for _, key := range []string{"MEMBER_NEWS_CLIPROXY_MODEL", "DB_SSLMODE", "DB_QUERY_EXEC_MODE", "OTEL_ENVIRONMENT"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			err := ValidateUnsupportedLegacyEnvUsage()
			if err == nil || !strings.Contains(err.Error(), key+" is retired") {
				t.Fatalf("ValidateUnsupportedLegacyEnvUsage() error = %v, want presence-based retirement error for %s", err, key)
			}
		})
	}
}
