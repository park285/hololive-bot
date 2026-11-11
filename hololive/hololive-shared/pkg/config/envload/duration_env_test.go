package envload

import (
	"strings"
	"testing"
	"time"
)

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
