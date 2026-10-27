package envload

import (
	"strings"
	"testing"
	"time"
)

// 필수 양수 기간 helper는 파싱 실패를 helper 이름 없이 한 번만 감싸, 오류가 키 이름과 원인에서 바로 시작하게 한다.
func TestRequiredDurationEnvReportsParseFailureOnce(t *testing.T) {
	const key = "ENVLOAD_TEST_REQUIRED_DURATION"

	t.Setenv(key, "abc")

	for name, load := range map[string]func(string, time.Duration) (time.Duration, error){
		"seconds": RequiredSecondsDurationEnv,
		"millis":  RequiredMillisDurationEnv,
	} {
		_, err := load(key, time.Second)
		if err == nil {
			t.Fatalf("%s: accepted non-numeric value", name)
		}

		const wantPrefix = "read int64 env: envutil: invalid int64 env " + key + " ("

		if got := err.Error(); !strings.HasPrefix(got, wantPrefix) {
			t.Fatalf("%s: error = %q, want prefix %q", name, got, wantPrefix)
		}
	}
}
