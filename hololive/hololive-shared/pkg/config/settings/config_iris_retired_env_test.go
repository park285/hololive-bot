package settings

import (
	"strings"
	"testing"
)

// 퇴역 가드의 fail-closed 계약을 고정한다. 제거 조건과 재검토 기한은 config_iris_retired_env.go 상단 주석이 소유하며,
// 가드를 삭제하는 리비전에서 이 테스트도 함께 삭제한다.
func TestRejectRetiredIrisEnvIsPresenceBased(t *testing.T) {
	for _, key := range retiredIrisEnvKeys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")

			if err := rejectRetiredIrisEnv(); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("rejectRetiredIrisEnv() error = %v, want %s rejected on presence with an empty value", err, key)
			}
		})
	}
}

func TestIrisRuntimeValidationAlwaysValidatesFileStatInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	// 퇴역한 우회 플래그가 프로세스 env에 남아 있어도 검사는 꺼지지 않는다(기동은 buildConfig의 가드가 먼저 막는다).
	t.Setenv("IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS", "true")

	if !LoadIrisRuntimeValidationConfig().ValidateFileStat {
		t.Fatal("production must always validate IRIS_BASE_URL_FILE stat")
	}
}
